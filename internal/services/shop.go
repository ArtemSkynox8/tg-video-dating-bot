package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/kinguin"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/payments"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/version"
)

type ShopService struct {
	cfg     config.Config
	repo    *repositories.Repository
	max     *maxapi.Client
	kinguin *kinguin.Client
	tbank   *payments.TBank
}

func NewShopService(cfg config.Config, repo *repositories.Repository, max *maxapi.Client, kinguinClient *kinguin.Client, tbank *payments.TBank) *ShopService {
	return &ShopService{cfg: cfg, repo: repo, max: max, kinguin: kinguinClient, tbank: tbank}
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
			"Kinguin product: "+order.KinguinProductID,
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
	return s.max.SendText(ctx, chatID, "Привет! Здесь можно купить Roblox Gift Card и получить код сразу после оплаты.\n\nbuild: "+version.Build, [][]maxapi.Button{
		{{Text: "Купить Робаксы", Payload: "buy"}},
	})
}

func (s *ShopService) sendNominals(ctx context.Context, chatID string) error {
	rows := [][]maxapi.Button{}
	for _, product := range s.cfg.Products {
		rows = append(rows, []maxapi.Button{{Text: product.Label, Payload: "nominal:" + product.Code}})
	}
	return s.max.SendText(ctx, chatID, "Выберите номинал: точную цену посчитаем перед оплатой.", rows)
}

func (s *ShopService) createOrder(ctx context.Context, user *models.User, code string) error {
	product, ok := s.productByCode(code)
	if !ok {
		return s.max.SendText(ctx, user.PlatformChatID, "Этот номинал пока не настроен.", nil)
	}
	retailID := product.KinguinRetailID
	if !validKinguinProductID(retailID) {
		return s.max.SendText(ctx, user.PlatformChatID, "Этот номинал пока не подключен к Kinguin.", nil)
	}
	if s.cfg.KinguinAPIKey == "" {
		return s.max.SendText(ctx, user.PlatformChatID, "Проверка цены Kinguin пока не настроена.", nil)
	}
	quote, err := s.kinguin.ResolveRetailProduct(ctx, retailID)
	if err != nil {
		log.Printf("kinguin retail lookup failed retail=%s nominal=%s: %v", retailID, product.Code, err)
		return s.max.SendText(ctx, user.PlatformChatID, "Не удалось проверить актуальную цену. Попробуйте позже.", nil)
	}
	productID := quote.ProductID
	if quote.Qty <= 0 {
		return s.max.SendText(ctx, user.PlatformChatID, "Товар временно закончился.", nil)
	}
	if quote.Price <= 0 {
		log.Printf("kinguin retail lookup returned empty price retail=%s product=%s nominal=%s currency=%s qty=%d", retailID, productID, product.Code, quote.Currency, quote.Qty)
		return s.max.SendText(ctx, user.PlatformChatID, "Не удалось получить цену товара. Попробуйте позже.", nil)
	}
	orderSum := s.calculateRUB(quote.Price, quote.Currency)
	order, err := s.repo.CreateOrder(ctx, models.Order{
		UserID:           user.ID,
		NominalCode:      product.Code,
		ProductLabel:     product.Label,
		KinguinProductID: productID,
		SourcePrice:      quote.Price,
		SourceCurrency:   strings.ToUpper(quote.Currency),
		OrderSum:         orderSum,
		Status:           models.OrderStatusCreated,
		PaymentProvider:  "tbank",
	})
	if err != nil {
		return err
	}
	if !s.tbank.Enabled() {
		return s.max.SendText(ctx, user.PlatformChatID, fmt.Sprintf("Заказ #%d создан на сумму %.0f руб., но T-Банк еще не настроен на сервере.", order.ID, orderSum), nil)
	}
	payment, err := s.tbank.Create(ctx, order.ID, orderSum, "Roblox Gift Card "+product.Label)
	if err != nil {
		_ = s.repo.MarkOrderError(ctx, order.ID, models.OrderStatusError, err.Error())
		return s.max.SendText(ctx, user.PlatformChatID, "Не удалось создать платеж. Попробуйте позже.", nil)
	}
	if err := s.repo.UpdateOrderPayment(ctx, order.ID, models.OrderStatusPending, payment.ID, payment.URL); err != nil {
		return err
	}
	return s.max.SendText(ctx, user.PlatformChatID,
		fmt.Sprintf("Счет на оплату: %s\nСумма к оплате: %.0f руб.\n\nПосле оплаты мы проверим заказ и выдадим код.", product.Label, orderSum),
		[][]maxapi.Button{{{Text: fmt.Sprintf("Оплатить %.0f руб.", orderSum), URL: payment.URL}}})
}

func (s *ShopService) calculateRUB(price float64, currency string) float64 {
	rate := s.cfg.USDRUBRate
	switch strings.ToUpper(currency) {
	case "EUR":
		rate = s.cfg.EURRUBRate
	case "RUB":
		rate = 1
	}
	sum := price*rate + s.cfg.FixedFeeRUB + s.cfg.DynamicMarginRUB
	if s.cfg.AcquiringFeePercent > 0 && s.cfg.AcquiringFeePercent < 100 {
		sum = sum / (1 - s.cfg.AcquiringFeePercent/100)
	}
	return roundUpToNine(sum)
}

func roundUpToNine(sum float64) float64 {
	rounded := int64(math.Ceil(sum))
	remainder := rounded % 10
	if remainder <= 9 {
		return float64(rounded + (9 - remainder))
	}
	return float64(rounded)
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

func validKinguinProductID(productID string) bool {
	productID = strings.TrimSpace(strings.ToLower(productID))
	if productID == "" {
		return false
	}
	invalidParts := []string{"replace", "id_товара", "product-id", "kinguin-product-id"}
	for _, part := range invalidParts {
		if strings.Contains(productID, part) {
			return false
		}
	}
	return true
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
