package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

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

func (s *ShopService) StartRestockWatcher(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkRestocks(ctx)
		}
	}
}

func (s *ShopService) checkRestocks(ctx context.Context) {
	if s.cfg.KinguinAPIKey == "" {
		return
	}
	for _, product := range s.cfg.Products {
		quote, err := s.kinguin.ResolveRetailProduct(ctx, product.KinguinRetailID)
		if err != nil || quote.Price <= 0 || quote.Qty <= 0 {
			continue
		}
		balance, err := s.kinguin.Balance(ctx, quote.Currency)
		if err != nil || balance < quote.Price {
			continue
		}
		if err := s.notifyWaitlistRestocked(ctx, product.Code, product.Label); err != nil {
			log.Printf("notify restock nominal=%s: %v", product.Code, err)
		}
	}
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
	if strings.HasPrefix(text, "/deliver") && s.isAdmin(msg.From.ID) {
		return s.deliverKinguinOrder(ctx, user.PlatformChatID, strings.Fields(msg.Text))
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
	result := kinguin.OrderResult{OrderID: order.KinguinOrderID}
	if order.KinguinOrderID != "" {
		result.Code, result.Details = s.kinguin.GetOrderCode(ctx, order.KinguinOrderID)
	} else {
		result, err = s.kinguin.CreateOrder(ctx, order.KinguinProductID, order.SourcePrice, fmt.Sprintf("max-%d", order.ID))
	}
	if err != nil {
		_ = s.repo.MarkOrderManualWithKinguinOrder(ctx, order.ID, result.OrderID, err.Error())
		_ = s.notifyAdmins(ctx, "Ошибка Kinguin после оплаты",
			"Order: "+fmt.Sprint(order.ID),
			"User: "+order.PlatformUserID,
			"Nominal: "+order.ProductLabel,
			"Kinguin product: "+order.KinguinProductID,
			"Kinguin source price: "+fmt.Sprintf("%.2f %s", order.SourcePrice, order.SourceCurrency),
			"Kinguin order: "+result.OrderID,
			"Payment: "+paymentID,
			"Error: "+err.Error(),
		)
		return s.max.SendText(ctx, order.PlatformChatID, "Произошла техническая задержка при генерации кода. Наш менеджер уже проверяет платеж и выдаст код вручную в течение 10 минут.", nil)
	}
	if result.Code == "" {
		errText := "Kinguin order created, but code was not found in response"
		if result.Details != "" {
			errText += ": " + result.Details
		}
		_ = s.repo.MarkOrderManualWithKinguinOrder(ctx, order.ID, result.OrderID, errText)
		_ = s.notifyAdmins(ctx, "Код Kinguin не найден в ответе",
			"Order: "+fmt.Sprint(order.ID),
			"Kinguin order: "+result.OrderID,
			"Kinguin response: "+result.Details,
			"User: "+order.PlatformUserID,
		)
		return s.max.SendText(ctx, order.PlatformChatID, "Оплата прошла, но код требует ручной проверки. Менеджер выдаст его в течение 10 минут.", nil)
	}
	if err := s.repo.MarkOrderSuccess(ctx, order.ID, result.OrderID, result.Code); err != nil {
		return err
	}
	return s.sendGiftCode(ctx, order.PlatformChatID, result.Code)
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
	balance, err := s.kinguin.Balance(ctx, quote.Currency)
	if err != nil {
		log.Printf("kinguin balance check failed nominal=%s currency=%s price=%.2f: %v", product.Code, quote.Currency, quote.Price, err)
		_ = s.notifyAdmins(ctx, "Не удалось проверить баланс Kinguin, счет выставлен без проверки баланса",
			"Nominal: "+product.Label,
			"User: "+user.PlatformUserID,
			"Kinguin product: "+productID,
			"Needed: "+fmt.Sprintf("%.2f %s", quote.Price, quote.Currency),
			"Error: "+err.Error(),
		)
	}
	if err == nil && balance < quote.Price {
		if err := s.repo.AddWaitlist(ctx, user.ID, product.Code, product.Label); err != nil {
			log.Printf("add waitlist user=%d nominal=%s: %v", user.ID, product.Code, err)
		}
		_ = s.notifyAdmins(ctx, "Не хватает баланса Kinguin для продажи",
			"Nominal: "+product.Label,
			"User: "+user.PlatformUserID,
			"Kinguin product: "+productID,
			"Needed: "+fmt.Sprintf("%.2f %s", quote.Price, quote.Currency),
			"Balance: "+fmt.Sprintf("%.2f %s", balance, quote.Currency),
		)
		return s.max.SendText(ctx, user.PlatformChatID, "Данный номинал карточек временно закончился. Повторите попытку позднее, мы напишем вам, когда карточки появятся в продаже.", nil)
	}
	_ = s.notifyWaitlistRestocked(ctx, product.Code, product.Label)
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

func (s *ShopService) deliverKinguinOrder(ctx context.Context, adminChatID string, fields []string) error {
	if len(fields) < 3 {
		return s.max.SendText(ctx, adminChatID, "Использование: /deliver <order_id> <kinguin_order_id>", nil)
	}
	orderID, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || orderID <= 0 {
		return s.max.SendText(ctx, adminChatID, "Некорректный order_id.", nil)
	}
	kinguinOrderID := strings.TrimSpace(fields[2])
	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return s.max.SendText(ctx, adminChatID, "Заказ не найден.", nil)
	}
	code, details := s.kinguin.GetOrderCode(ctx, kinguinOrderID)
	if code == "" {
		_ = s.repo.MarkOrderManualWithKinguinOrder(ctx, order.ID, kinguinOrderID, "manual deliver failed: "+details)
		return s.max.SendText(ctx, adminChatID, "Код пока не найден в Kinguin order "+kinguinOrderID+". Ответ: "+details, nil)
	}
	if err := s.repo.MarkOrderSuccess(ctx, order.ID, kinguinOrderID, code); err != nil {
		return err
	}
	if err := s.sendGiftCode(ctx, order.PlatformChatID, code); err != nil {
		return err
	}
	return s.max.SendText(ctx, adminChatID, fmt.Sprintf("Код по заказу #%d отправлен пользователю.", order.ID), nil)
}

func (s *ShopService) sendGiftCode(ctx context.Context, chatID, code string) error {
	if err := s.max.SendText(ctx, chatID, "Спасибо за оплату!\nВаш код активации:", nil); err != nil {
		return err
	}
	if err := s.max.SendText(ctx, chatID, code, nil); err != nil {
		return err
	}
	return s.max.SendText(ctx, chatID, giftCodeInstructionText(), nil)
}

func giftCodeInstructionText() string {
	return "Краткая инструкция:\n" +
		"• Перейдите в браузере на страницу roblox.com/redeem.\n" +
		"• Войдите в свой аккаунт Roblox.\n" +
		"• Введите полученный 16-значный код в поле и нажмите кнопку Redeem."
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

func (s *ShopService) notifyWaitlistRestocked(ctx context.Context, nominalCode, productLabel string) error {
	items, err := s.repo.WaitlistByNominal(ctx, nominalCode)
	if err != nil {
		return err
	}
	ids := []int64{}
	for _, item := range items {
		if item.PlatformChatID == "" {
			continue
		}
		if err := s.max.SendText(ctx, item.PlatformChatID, productLabel+" снова появился в продаже. Можно вернуться в бот и оформить покупку.", [][]maxapi.Button{
			{{Text: "Купить "+productLabel, Payload: "nominal:" + nominalCode}},
		}); err != nil {
			log.Printf("notify waitlist user=%s nominal=%s: %v", item.PlatformUserID, nominalCode, err)
			continue
		}
		ids = append(ids, item.ID)
	}
	if len(ids) > 0 {
		return s.repo.MarkWaitlistNotified(ctx, ids)
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
