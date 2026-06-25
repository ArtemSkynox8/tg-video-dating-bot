package services

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/kinguin"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/payments"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
)

type ShopService struct {
	cfg     config.Config
	repo    *repositories.Repository
	max     *maxapi.Client
	kinguin *kinguin.Client
	yookassa *payments.YooKassa
}

func NewShopService(cfg config.Config, repo *repositories.Repository, max *maxapi.Client, kinguinClient *kinguin.Client, yookassa *payments.YooKassa) *ShopService {
	return &ShopService{cfg: cfg, repo: repo, max: max, kinguin: kinguinClient, yookassa: yookassa}
}

func (s *ShopService) HandleMessage(ctx context.Context, msg maxapi.MessageUpdate) error {
	user, err := s.repo.UpsertUser(ctx, userFromMessage(msg))
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.ToLower(msg.Text))
	if strings.HasPrefix(text, "/stats") && s.isAdmin(msg.From.ID) {
		return s.sendStats(ctx, user.PlatformChatID)
	}
	return s.sendStart(ctx, user.PlatformChatID)
}

func (s *ShopService) HandleCallback(ctx context.Context, cb maxapi.CallbackUpdate) error {
	user, err := s.repo.UpsertUser(ctx, models.User{
		PlatformUserID: cb.From.ID,
		PlatformChatID: firstNonEmpty(cb.Chat.ID, cb.From.ID),
		Username:       cb.From.Username,
		Name:           displayName(cb.From),
	})
	if err != nil {
		return err
	}
	_ = s.max.AnswerCallback(ctx, cb.CallbackID, "Принято")

	switch {
	case cb.Payload == "buy":
		return s.sendNominals(ctx, user.PlatformChatID)
	case strings.HasPrefix(cb.Payload, "nominal:"):
		return s.createOrder(ctx, user, strings.TrimPrefix(cb.Payload, "nominal:"))
	default:
		return s.sendStart(ctx, user.PlatformChatID)
	}
}

func (s *ShopService) CompletePaidOrder(ctx context.Context, orderID int64, paymentID string) error {
	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if order.Status == models.OrderStatusSuccess {
		return nil
	}
	if err := s.repo.MarkOrderPaid(ctx, order.ID, paymentID); err != nil {
		return err
	}
	if s.cfg.KinguinAPIKey == "" || strings.HasPrefix(order.KinguinProductID, "stub-") {
		if err := s.repo.MarkOrderError(ctx, order.ID, models.OrderStatusManual, "manual delivery mode"); err != nil {
			return err
		}
		_ = s.notifyAdmins(ctx, "Оплачен заказ Roblox Gift Card",
			"Order: "+fmt.Sprint(order.ID),
			"User: "+order.PlatformUserID,
			"Nominal: "+order.ProductLabel,
			"Amount: "+fmt.Sprintf("%.0f руб.", order.OrderSum),
			"Payment: "+paymentID,
		)
		return s.max.SendText(ctx, order.PlatformChatID, fmt.Sprintf("Оплата получена. Заказ #%d: %s на сумму %.0f руб.\n\nКод будет выдан вручную в ближайшее время.", order.ID, order.ProductLabel, order.OrderSum), nil)
	}
	result, err := s.kinguin.CreateOrder(ctx, order.KinguinProductID, fmt.Sprintf("max-%d", order.ID))
	if err != nil {
		_ = s.repo.MarkOrderError(ctx, order.ID, models.OrderStatusManual, err.Error())
		_ = s.notifyAdmins(ctx, "Ошибка Kinguin после оплаты",
			"Order: "+fmt.Sprint(order.ID),
			"User: "+order.PlatformUserID,
			"Nominal: "+order.ProductLabel,
			"Payment: "+paymentID,
			"Error: "+err.Error(),
		)
		return s.max.SendText(ctx, order.PlatformChatID, "Произошла техническая задержка при генерации кода. Наш менеджер уже проверяет платеж и выдаст код вручную в течение 10 минут.", nil)
	}
	if result.Code == "" {
		errText := "Kinguin order created, but code was not found in response"
		_ = s.repo.MarkOrderError(ctx, order.ID, models.OrderStatusManual, errText)
		_ = s.notifyAdmins(ctx, "Код Kinguin не найден в ответе",
			"Order: "+fmt.Sprint(order.ID),
			"Kinguin order: "+result.OrderID,
			"User: "+order.PlatformUserID,
		)
		return s.max.SendText(ctx, order.PlatformChatID, "Оплата прошла, но код требует ручной проверки. Менеджер выдаст его в течение 10 минут.", nil)
	}
	if err := s.repo.MarkOrderSuccess(ctx, order.ID, result.OrderID, result.Code); err != nil {
		return err
	}
	return s.max.SendFormattedText(ctx, order.PlatformChatID,
		"Спасибо за оплату! Ваш код активации:<br><code>"+escapeHTML(result.Code)+"</code><br><br>Инструкция: откройте страницу активации Roblox Gift Cards, войдите в аккаунт Roblox, введите код и подтвердите активацию.",
		"Спасибо за оплату! Ваш код активации:\n"+result.Code+"\n\nИнструкция: откройте страницу активации Roblox Gift Cards, войдите в аккаунт Roblox, введите код и подтвердите активацию.",
		nil)
}

func (s *ShopService) sendStart(ctx context.Context, chatID string) error {
	return s.max.SendText(ctx, chatID, "Привет! Здесь можно купить Roblox Gift Card и получить код сразу после оплаты.", [][]maxapi.Button{
		{{Text: "Купить Робаксы", Payload: "buy"}},
	})
}

func (s *ShopService) sendNominals(ctx context.Context, chatID string) error {
	rows := [][]maxapi.Button{}
	for _, product := range s.cfg.Products {
		rows = append(rows, []maxapi.Button{{Text: fmt.Sprintf("%s - %.0f руб.", product.Label, product.PriceRUB), Payload: "nominal:" + product.Code}})
	}
	return s.max.SendText(ctx, chatID, "Выберите номинал:", rows)
}

func (s *ShopService) createOrder(ctx context.Context, user *models.User, code string) error {
	product, ok := s.productByCode(code)
	if !ok {
		return s.max.SendText(ctx, user.PlatformChatID, "Этот номинал пока не настроен.", nil)
	}
	orderSum := product.PriceRUB
	productID := product.KinguinProductID
	if productID == "" {
		productID = "stub-" + product.Code
	}
	order, err := s.repo.CreateOrder(ctx, models.Order{
		UserID:           user.ID,
		NominalCode:      product.Code,
		ProductLabel:     product.Label,
		KinguinProductID: productID,
		SourcePrice:      orderSum,
		SourceCurrency:   "RUB",
		OrderSum:         orderSum,
		Status:           models.OrderStatusCreated,
		PaymentProvider:  "yookassa",
	})
	if err != nil {
		return err
	}
	if !s.yookassa.Enabled() {
		return s.max.SendText(ctx, user.PlatformChatID, fmt.Sprintf("Заказ #%d создан на сумму %.0f руб., но YooKassa еще не настроена на сервере.", order.ID, orderSum), nil)
	}
	payment, err := s.yookassa.Create(ctx, order.ID, orderSum, "Roblox Gift Card "+product.Label)
	if err != nil {
		_ = s.repo.MarkOrderError(ctx, order.ID, models.OrderStatusError, err.Error())
		return s.max.SendText(ctx, user.PlatformChatID, "Не удалось создать платеж. Попробуйте позже.", nil)
	}
	if err := s.repo.UpdateOrderPayment(ctx, order.ID, models.OrderStatusPending, payment.ID, payment.URL); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID,
		fmt.Sprintf("Счет на оплату: %s\nСумма к оплате: %.0f руб.\n\nПосле оплаты мы проверим заказ и выдадим код.", product.Label, orderSum),
		[][]maxapi.Button{{{Text: "Оплатить", URL: payment.URL}}})
}

func (s *ShopService) calculateRUB(price float64, currency string) float64 {
	rate := s.cfg.USDRUBRate
	switch strings.ToUpper(currency) {
	case "EUR":
		rate = s.cfg.EURRUBRate
	case "RUB":
		rate = 1
	}
	sum := price*rate*(1+s.cfg.MarkupPercent/100) + s.cfg.FixedFeeRUB
	step := math.Max(1, s.cfg.RoundToRUB)
	return math.Ceil(sum/step) * step
}

func (s *ShopService) sendStats(ctx context.Context, chatID string) error {
	stats, err := s.repo.Stats(ctx)
	if err != nil {
		return err
	}
	lines := []string{"Статистика заказов:"}
	for _, status := range []string{models.OrderStatusCreated, models.OrderStatusPending, models.OrderStatusPaid, models.OrderStatusSuccess, models.OrderStatusError, models.OrderStatusManual} {
		lines = append(lines, status+": "+strconv.FormatInt(stats[status], 10))
	}
	return s.max.SendText(ctx, chatID, strings.Join(lines, "\n"), nil)
}

func (s *ShopService) notifyAdmins(ctx context.Context, lines ...string) error {
	if len(s.cfg.AdminPlatformIDs) == 0 {
		return nil
	}
	text := strings.Join(lines, "\n")
	for _, adminID := range s.cfg.AdminPlatformIDs {
		_ = s.max.SendText(ctx, adminID, text, nil)
	}
	return nil
}

func (s *ShopService) productByCode(code string) (config.Product, bool) {
	for _, product := range s.cfg.Products {
		if product.Code == code {
			return product, true
		}
	}
	return config.Product{}, false
}

func (s *ShopService) isAdmin(userID string) bool {
	for _, adminID := range s.cfg.AdminPlatformIDs {
		if adminID == userID {
			return true
		}
	}
	return false
}

func userFromMessage(msg maxapi.MessageUpdate) models.User {
	return models.User{
		PlatformUserID: msg.From.ID,
		PlatformChatID: firstNonEmpty(msg.Chat.ID, msg.From.ID),
		Username:       msg.From.Username,
		Name:           displayName(msg.From),
	}
}

func displayName(user maxapi.PlatformUser) string {
	return firstNonEmpty(user.Name, strings.TrimSpace(user.FirstName+" "+user.LastName), user.Username)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func escapeHTML(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return replacer.Replace(value)
}
