package repositories

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

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
		insert into users (platform_user_id, platform_chat_id, username, name, ad_tag)
		values ($1, $2, nullif($3, ''), nullif($4, ''), coalesce(nullif($5, ''), 'direct'))
		on conflict (platform_user_id) do update set
			platform_chat_id = excluded.platform_chat_id,
			username = coalesce(excluded.username, users.username),
			name = coalesce(excluded.name, users.name),
			ad_tag = case when users.ad_tag='direct' and excluded.ad_tag <> 'direct' then excluded.ad_tag else users.ad_tag end,
			updated_at = now()
		returning id, platform_user_id, platform_chat_id, coalesce(username, ''), coalesce(name, ''),
			coalesce(ad_tag, 'direct'), (xmax = 0), created_at, updated_at`,
		user.PlatformUserID, user.PlatformChatID, user.Username, user.Name, user.AdTag)
	var out models.User
	err := row.Scan(&out.ID, &out.PlatformUserID, &out.PlatformChatID, &out.Username, &out.Name, &out.AdTag, &out.IsNew, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
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

func (r *Repository) WalletBalance(ctx context.Context, currency string) (float64, error) {
	var amount float64
	err := r.db.QueryRow(ctx, `
		select amount from wallet_balances where currency=$1`, currency).Scan(&amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return amount, err
}

func (r *Repository) SetWalletBalance(ctx context.Context, currency string, amount float64) (float64, error) {
	var current float64
	err := r.db.QueryRow(ctx, `
		insert into wallet_balances (currency, amount)
		values ($1,$2)
		on conflict (currency) do update set amount=excluded.amount, updated_at=now()
		returning amount`, currency, amount).Scan(&current)
	return current, err
}

func (r *Repository) AddWalletBalance(ctx context.Context, currency string, amount float64) (float64, error) {
	var current float64
	err := r.db.QueryRow(ctx, `
		insert into wallet_balances (currency, amount)
		values ($1,$2)
		on conflict (currency) do update set amount=wallet_balances.amount+excluded.amount, updated_at=now()
		returning amount`, currency, amount).Scan(&current)
	return current, err
}

func (r *Repository) DebitWalletForOrder(ctx context.Context, orderID int64, currency string, amount float64) (float64, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)

	var insertedOrderID int64
	err = tx.QueryRow(ctx, `
		insert into wallet_debits (order_id, currency, amount)
		values ($1,$2,$3)
		on conflict (order_id) do nothing
		returning order_id`, orderID, currency, amount).Scan(&insertedOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		var current float64
		if err := tx.QueryRow(ctx, `select amount from wallet_balances where currency=$1`, currency).Scan(&current); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, false, tx.Commit(ctx)
			}
			return 0, false, err
		}
		return current, false, tx.Commit(ctx)
	}
	if err != nil {
		return 0, false, err
	}

	var current float64
	err = tx.QueryRow(ctx, `
		insert into wallet_balances (currency, amount)
		values ($1, 0-$2)
		on conflict (currency) do update set amount=wallet_balances.amount-$2, updated_at=now()
		returning amount`, currency, amount).Scan(&current)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return current, true, nil
}

func (r *Repository) AddWaitlist(ctx context.Context, userID int64, nominalCode, productLabel string) error {
	_, err := r.db.Exec(ctx, `
		insert into restock_waitlist (user_id, nominal_code, product_label)
		values ($1,$2,$3)
		on conflict (user_id, nominal_code) do update set
			product_label=excluded.product_label,
			notified_at=null,
			updated_at=now()`, userID, nominalCode, productLabel)
	return err
}

func (r *Repository) WaitlistByNominal(ctx context.Context, nominalCode string) ([]models.WaitlistEntry, error) {
	rows, err := r.db.Query(ctx, `
		select w.id, w.user_id, u.platform_user_id, u.platform_chat_id, w.nominal_code, w.product_label
		from restock_waitlist w join users u on u.id=w.user_id
		where w.nominal_code=$1 and w.notified_at is null
		order by w.created_at`, nominalCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.WaitlistEntry{}
	for rows.Next() {
		var item models.WaitlistEntry
		if err := rows.Scan(&item.ID, &item.UserID, &item.PlatformUserID, &item.PlatformChatID, &item.NominalCode, &item.ProductLabel); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) MarkWaitlistNotified(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if _, err := r.db.Exec(ctx, `
			update restock_waitlist set notified_at=now(), updated_at=now()
			where id=$1`, id); err != nil {
			return err
		}
	}
	return nil
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

func (r *Repository) RecordEvent(ctx context.Context, user *models.User, eventType, details string) error {
	if user == nil {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		insert into user_events (user_id, platform_user_id, ad_tag, event_type, details)
		values ($1,$2,$3,$4,nullif($5, ''))`,
		user.ID, user.PlatformUserID, emptyDefault(user.AdTag, "direct"), eventType, details)
	return err
}

func (r *Repository) EventStats(ctx context.Context, tag string) ([]models.EventStat, error) {
	query := `select event_type, count(*) from user_events`
	args := []any{}
	if strings.TrimSpace(tag) != "" {
		query += ` where ad_tag=$1`
		args = append(args, strings.TrimSpace(tag))
	}
	query += ` group by event_type order by event_type`
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.EventStat{}
	for rows.Next() {
		var item models.EventStat
		if err := rows.Scan(&item.Name, &item.Count); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) AdStats(ctx context.Context, tag string) (models.AdStat, error) {
	tag = strings.TrimSpace(tag)
	out := models.AdStat{Tag: tag}
	args := []any{}
	userFilter := ``
	eventFilter := `where user_id is not null`
	if tag != "" {
		userFilter = `where coalesce(nullif(ad_tag, ''), 'direct')=$1`
		eventFilter = `where coalesce(nullif(ad_tag, ''), 'direct')=$1 and user_id is not null`
		args = append(args, tag)
	}
	query := `
		with tagged_users as (
			select id from users ` + userFilter + `
			union
			select user_id from user_events ` + eventFilter + `
		),
		paid_orders as (
			select o.user_id, o.order_sum
			from orders o join tagged_users tu on tu.id=o.user_id
			where o.status in ($` + strconv.Itoa(len(args)+1) + `,$` + strconv.Itoa(len(args)+2) + `,$` + strconv.Itoa(len(args)+3) + `)
		)
		select
			(select count(*) from tagged_users),
			(select count(distinct user_id) from paid_orders),
			coalesce((select sum(order_sum) from paid_orders), 0)`
	args = append(args, models.OrderStatusPaid, models.OrderStatusSuccess, models.OrderStatusManual)
	err := r.db.QueryRow(ctx, query, args...).Scan(&out.Users, &out.Paid, &out.Revenue)
	return out, err
}

func (r *Repository) AdStatsByTag(ctx context.Context) ([]models.AdStat, error) {
	rows, err := r.db.Query(ctx, `
		select tag from (
			select distinct coalesce(nullif(ad_tag, ''), 'direct') as tag from users
			union
			select distinct coalesce(nullif(ad_tag, ''), 'direct') as tag from user_events
		) t
		where tag <> ''
		order by tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]models.AdStat, 0, len(tags))
	for _, tag := range tags {
		item, err := r.AdStats(ctx, tag)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *Repository) ChoiceStats(ctx context.Context) ([]models.EventStat, error) {
	rows, err := r.db.Query(ctx, `
		select coalesce(nullif(details, ''), 'unknown'), count(*)
		from user_events
		where event_type='nominal_selected'
		group by details
		order by count(*) desc, details`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.EventStat{}
	for rows.Next() {
		var item models.EventStat
		if err := rows.Scan(&item.Name, &item.Count); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ActiveUsers(ctx context.Context) ([]models.User, error) {
	rows, err := r.db.Query(ctx, `
		select id, platform_user_id, platform_chat_id, coalesce(username, ''), coalesce(name, ''),
			coalesce(ad_tag, 'direct'), created_at, updated_at
		from users
		where platform_chat_id <> ''
		order by updated_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.User{}
	for rows.Next() {
		var user models.User
		if err := rows.Scan(&user.ID, &user.PlatformUserID, &user.PlatformChatID, &user.Username, &user.Name, &user.AdTag, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

func (r *Repository) LogPush(ctx context.Context, text string, sent, failed int) error {
	_, err := r.db.Exec(ctx, `
		insert into push_logs (text, sent_count, error_count)
		values ($1,$2,$3)`, text, sent, failed)
	return err
}

func (r *Repository) PushStats(ctx context.Context) (string, error) {
	var users int64
	if err := r.db.QueryRow(ctx, `select count(*) from users`).Scan(&users); err != nil {
		return "", err
	}
	var events int64
	if err := r.db.QueryRow(ctx, `select count(*) from user_events`).Scan(&events); err != nil {
		return "", err
	}
	var text string
	var sent, failed int
	err := r.db.QueryRow(ctx, `
		select coalesce(text, ''), sent_count, error_count
		from push_logs
		order by created_at desc
		limit 1`).Scan(&text, &sent, &failed)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Sprintf("Users: %d\nEvents: %d\nLast push: none", users, events), nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Users: %d\nEvents: %d\nLast push sent: %d\nLast push errors: %d\nText: %s", users, events, sent, failed, trimForAdmin(text, 120)), nil
}

func (r *Repository) RecentPayments(ctx context.Context, limit int) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		select id, product_label, order_sum, status, coalesce(payment_id, ''),
			coalesce(kinguin_product_id, ''), source_price, source_currency
		from orders
		where payment_id is not null
		order by updated_at desc
		limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id int64
		var label, status, paymentID, kinguinProductID, sourceCurrency string
		var sum, sourcePrice float64
		if err := rows.Scan(&id, &label, &sum, &status, &paymentID, &kinguinProductID, &sourcePrice, &sourceCurrency); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("#%d %s %.0f руб. %s cost=%.2f %s product=%s payment=%s", id, label, sum, status, sourcePrice, sourceCurrency, kinguinProductID, paymentID))
	}
	return out, rows.Err()
}

func (r *Repository) RecentErrors(ctx context.Context, limit int) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		select id, product_label, status, coalesce(kinguin_order_id, ''), coalesce(error_text, '')
		from orders
		where error_text is not null and error_text <> ''
		order by updated_at desc
		limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id int64
		var label, status, kinguinOrderID, errorText string
		if err := rows.Scan(&id, &label, &status, &kinguinOrderID, &errorText); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("#%d %s %s kinguin=%s error=%s", id, label, status, kinguinOrderID, trimForAdmin(errorText, 90)))
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
	err := row.Scan(&user.ID, &user.PlatformUserID, &user.PlatformChatID, &user.Username, &user.Name, &user.AdTag, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &user, err
}

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func trimForAdmin(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
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
