package repositories

import (
	"context"
	"errors"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Repository struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) UpsertUser(ctx context.Context, user models.User) (*models.User, error) {
	row := r.db.QueryRow(ctx, `
		insert into users (platform_user_id, platform_chat_id, username, name)
		values ($1, $2, nullif($3, ''), nullif($4, ''))
		on conflict (platform_user_id) do update set
			platform_chat_id = excluded.platform_chat_id,
			username = coalesce(excluded.username, users.username),
			name = coalesce(excluded.name, users.name),
			updated_at = now()
		returning id, platform_user_id, platform_chat_id, coalesce(username, ''), coalesce(name, ''), created_at, updated_at`,
		user.PlatformUserID, user.PlatformChatID, user.Username, user.Name)
	return scanUser(row)
}

func (r *Repository) CreateOrder(ctx context.Context, order models.Order) (*models.Order, error) {
	row := r.db.QueryRow(ctx, `
		insert into orders (
			user_id, nominal_code, product_label, kinguin_product_id, source_price,
			source_currency, order_sum, status, payment_provider
		)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		returning id, user_id, nominal_code, product_label, kinguin_product_id, source_price,
			source_currency, order_sum, status, coalesce(payment_provider, ''), coalesce(payment_id, ''),
			coalesce(payment_url, ''), coalesce(kinguin_order_id, ''), coalesce(gift_code, ''),
			coalesce(error_text, ''), created_at, updated_at`,
		order.UserID, order.NominalCode, order.ProductLabel, order.KinguinProductID, order.SourcePrice,
		order.SourceCurrency, order.OrderSum, order.Status, order.PaymentProvider)
	return scanOrder(row)
}

func (r *Repository) UpdateOrderPayment(ctx context.Context, orderID int64, status, paymentID, paymentURL string) error {
	_, err := r.db.Exec(ctx, `
		update orders set status=$2, payment_id=nullif($3, ''), payment_url=nullif($4, ''), updated_at=now()
		where id=$1`, orderID, status, paymentID, paymentURL)
	return err
}

func (r *Repository) GetOrder(ctx context.Context, orderID int64) (*models.Order, error) {
	row := r.db.QueryRow(ctx, orderSelect()+` where o.id=$1`, orderID)
	return scanOrderWithUser(row)
}

func (r *Repository) GetOrderByPaymentID(ctx context.Context, paymentID string) (*models.Order, error) {
	row := r.db.QueryRow(ctx, orderSelect()+` where o.payment_id=$1`, paymentID)
	return scanOrderWithUser(row)
}

func (r *Repository) MarkOrderPaid(ctx context.Context, orderID int64, paymentID string) error {
	_, err := r.db.Exec(ctx, `
		update orders set status=$2, payment_id=coalesce(nullif($3, ''), payment_id), updated_at=now()
		where id=$1 and status in ($4,$5)`,
		orderID, models.OrderStatusPaid, paymentID, models.OrderStatusCreated, models.OrderStatusPending)
	return err
}

func (r *Repository) MarkOrderSuccess(ctx context.Context, orderID int64, kinguinOrderID, giftCode string) error {
	_, err := r.db.Exec(ctx, `
		update orders set status=$2, kinguin_order_id=nullif($3, ''), gift_code=$4, error_text=null, updated_at=now()
		where id=$1`, orderID, models.OrderStatusSuccess, kinguinOrderID, giftCode)
	return err
}

func (r *Repository) MarkOrderManualWithKinguinOrder(ctx context.Context, orderID int64, kinguinOrderID, errorText string) error {
	_, err := r.db.Exec(ctx, `
		update orders set status=$2, kinguin_order_id=coalesce(nullif($3, ''), kinguin_order_id), error_text=$4, updated_at=now()
		where id=$1`, orderID, models.OrderStatusManual, kinguinOrderID, errorText)
	return err
}

func (r *Repository) MarkOrderError(ctx context.Context, orderID int64, status, errorText string) error {
	_, err := r.db.Exec(ctx, `
		update orders set status=$2, error_text=$3, updated_at=now()
		where id=$1`, orderID, status, errorText)
	return err
}

func (r *Repository) Stats(ctx context.Context) (map[string]int64, error) {
	rows, err := r.db.Query(ctx, `select status, count(*) from orders group by status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	return out, rows.Err()
}

func orderSelect() string {
	return `
		select o.id, o.user_id, u.platform_user_id, u.platform_chat_id, o.nominal_code, o.product_label,
			o.kinguin_product_id, o.source_price, o.source_currency, o.order_sum, o.status,
			coalesce(o.payment_provider, ''), coalesce(o.payment_id, ''), coalesce(o.payment_url, ''),
			coalesce(o.kinguin_order_id, ''), coalesce(o.gift_code, ''), coalesce(o.error_text, ''),
			o.created_at, o.updated_at
		from orders o join users u on u.id=o.user_id`
}

func scanUser(row pgx.Row) (*models.User, error) {
	var user models.User
	err := row.Scan(&user.ID, &user.PlatformUserID, &user.PlatformChatID, &user.Username, &user.Name, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &user, err
}

func scanOrder(row pgx.Row) (*models.Order, error) {
	var order models.Order
	err := row.Scan(&order.ID, &order.UserID, &order.NominalCode, &order.ProductLabel, &order.KinguinProductID,
		&order.SourcePrice, &order.SourceCurrency, &order.OrderSum, &order.Status, &order.PaymentProvider,
		&order.PaymentID, &order.PaymentURL, &order.KinguinOrderID, &order.GiftCode, &order.ErrorText,
		&order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &order, err
}

func scanOrderWithUser(row pgx.Row) (*models.Order, error) {
	var order models.Order
	err := row.Scan(&order.ID, &order.UserID, &order.PlatformUserID, &order.PlatformChatID, &order.NominalCode,
		&order.ProductLabel, &order.KinguinProductID, &order.SourcePrice, &order.SourceCurrency,
		&order.OrderSum, &order.Status, &order.PaymentProvider, &order.PaymentID, &order.PaymentURL,
		&order.KinguinOrderID, &order.GiftCode, &order.ErrorText, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &order, err
}
