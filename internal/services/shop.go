package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

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
		if product.MaxKinguinPriceUSD > 0 && s.sourcePriceUSD(quote.Price, normalizeCurrency(quote.Currency)) > product.MaxKinguinPriceUSD {
			continue
		}
		balance, err := s.repo.WalletBalance(ctx, normalizeCurrency(quote.Currency))
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
	rawText := strings.TrimSpace(msg.Text)
	text := strings.TrimSpace(strings.ToLower(rawText))
	if user.IsNew {
		_ = s.recordUserEvent(ctx, user, "new_user", "start")
		_ = s.notifyFunnelEvent(ctx, user, "New user", "start", "first")
	}
	if text == "/support" || text == "поддержка" {
		return s.sendSupport(ctx, user.PlatformChatID)
	}
	if text == "/admin" && s.isAdmin(msg.From.ID) {
		return s.sendAdminMenu(ctx, user.PlatformChatID)
	}
	if strings.HasPrefix(text, "/stats") && s.isAdmin(msg.From.ID) {
		return s.sendStats(ctx, user.PlatformChatID)
	}
	if strings.HasPrefix(text, "/botstats") && s.isAdmin(msg.From.ID) {
		return s.sendBotStats(ctx, user.PlatformChatID)
	}
	if strings.HasPrefix(text, "/adstats_all") && s.isAdmin(msg.From.ID) {
		return s.sendAdStats(ctx, user.PlatformChatID, "")
	}
	if strings.HasPrefix(text, "/adstats") && s.isAdmin(msg.From.ID) {
		return s.sendAdStats(ctx, user.PlatformChatID, strings.TrimSpace(strings.TrimPrefix(rawText, "/adstats")))
	}
	if strings.HasPrefix(text, "/choicestats") && s.isAdmin(msg.From.ID) {
		return s.sendChoiceStats(ctx, user.PlatformChatID)
	}
	if strings.HasPrefix(text, "/adtag") && s.isAdmin(msg.From.ID) {
		return s.createAdTag(ctx, user.PlatformChatID, strings.TrimSpace(strings.TrimPrefix(rawText, "/adtag")))
	}
	if strings.HasPrefix(text, "/push_stats") && s.isAdmin(msg.From.ID) {
		return s.sendPushStats(ctx, user.PlatformChatID)
	}
	if strings.HasPrefix(text, "/push") && s.isAdmin(msg.From.ID) {
		return s.sendPush(ctx, user.PlatformChatID, strings.TrimSpace(strings.TrimPrefix(rawText, "/push")))
	}
	if strings.HasPrefix(text, "/payments") && s.isAdmin(msg.From.ID) {
		return s.sendRecentPayments(ctx, user.PlatformChatID)
	}
	if strings.HasPrefix(text, "/errors") && s.isAdmin(msg.From.ID) {
		return s.sendRecentErrors(ctx, user.PlatformChatID)
	}
	if (text == "/balance" || text == "баланс") && s.isAdmin(msg.From.ID) {
		return s.sendWalletBalance(ctx, user.PlatformChatID)
	}
	if (strings.HasPrefix(text, "/setbalance") || strings.HasPrefix(text, "указать баланс")) && s.isAdmin(msg.From.ID) {
		return s.setWalletBalance(ctx, user.PlatformChatID, rawText)
	}
	if (strings.HasPrefix(text, "/addbalance") || strings.HasPrefix(text, "пополнить баланс")) && s.isAdmin(msg.From.ID) {
		return s.addWalletBalance(ctx, user.PlatformChatID, rawText)
	}
	if strings.HasPrefix(text, "/deliver") && s.isAdmin(msg.From.ID) {
		return s.deliverKinguinOrder(ctx, user.PlatformChatID, strings.Fields(rawText))
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
		_ = s.recordUserEvent(ctx, user, "buy_clicked", "buy")
		_ = s.notifyFunnelEvent(ctx, user, "Offer reached", "buy_clicked", "button")
		return s.sendNominals(ctx, user.PlatformChatID)
	case strings.HasPrefix(cb.Payload, "nominal:"):
		code := strings.TrimPrefix(cb.Payload, "nominal:")
		_ = s.recordUserEvent(ctx, user, "nominal_selected", code)
		_ = s.notifyFunnelEvent(ctx, user, "Nominal selected", code, "button")
		return s.createOrder(ctx, user, code)
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
		_ = s.recordUserEvent(ctx, orderUser(order), "kinguin_error", order.ProductLabel)
		_ = s.notifyAdmins(ctx, "Ошибка Kinguin после оплаты",
			"Order: "+fmt.Sprint(order.ID),
			"Nominal: "+order.ProductLabel,
			"Kinguin order: "+result.OrderID,
			"Payment: "+paymentID,
			"Error: "+shortError(err),
		)
		return s.max.SendText(ctx, order.PlatformChatID, "Произошла техническая задержка при генерации кода. Наш менеджер уже проверяет платеж и выдаст код вручную в течение 10 минут.", nil)
	}
	if result.Code == "" {
		errText := "Kinguin order created, but code was not found in response"
		if result.Details != "" {
			errText += ": " + result.Details
		}
		_ = s.repo.MarkOrderManualWithKinguinOrder(ctx, order.ID, result.OrderID, errText)
		log.Printf("kinguin code not found order=%d kinguin_order=%s details=%s", order.ID, result.OrderID, result.Details)
		_ = s.recordUserEvent(ctx, orderUser(order), "kinguin_code_missing", order.ProductLabel)
		_ = s.notifyAdmins(ctx, "Код Kinguin не найден",
			"Order: "+fmt.Sprint(order.ID),
			"Kinguin order: "+result.OrderID,
			"Action: проверь заказ в Kinguin и отправь /deliver",
		)
		return s.max.SendText(ctx, order.PlatformChatID, "Оплата прошла, но код требует ручной проверки. Менеджер выдаст его в течение 10 минут.", nil)
	}
	if err := s.markOrderSuccessAndDebit(ctx, order, result.OrderID, result.Code); err != nil {
		return err
	}
	_ = s.recordUserEvent(ctx, orderUser(order), "payment_success", order.ProductLabel)
	return s.sendGiftCode(ctx, order.PlatformChatID, result.Code)
}

func (s *ShopService) sendStart(ctx context.Context, chatID string) error {
	return s.max.SendText(ctx, chatID, "Привет! Здесь можно купить Roblox Gift Card и получить код сразу после оплаты.", [][]maxapi.Button{
		{{Text: "Купить Робаксы", Payload: "buy"}},
		{{Text: "Поддержка", URL: s.cfg.SupportURL}},
	})
}

func (s *ShopService) sendSupport(ctx context.Context, chatID string) error {
	return s.max.SendText(ctx, chatID, "По вопросам пишите сюда.", [][]maxapi.Button{
		{{Text: "Написать", URL: s.cfg.SupportURL}},
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
	if ok, reason := safeKinguinProduct(quote); !ok {
		log.Printf("kinguin product blocked retail=%s nominal=%s product=%s name=%q type=%q region=%q reason=%s", retailID, product.Code, quote.ProductID, quote.Name, quote.ItemType, quote.Region, reason)
		_ = s.notifyAdmins(ctx, "Kinguin товар заблокирован фильтром",
			"Nominal: "+product.Label,
			"Product: "+quote.ProductID,
			"Reason: "+reason,
		)
		return s.max.SendText(ctx, user.PlatformChatID, "Товар обновляется, пожалуйста, попробуйте через пару минут.", nil)
	}
	productID := quote.ProductID
	if quote.Qty <= 0 {
		return s.max.SendText(ctx, user.PlatformChatID, "Товар временно закончился.", nil)
	}
	if quote.Price <= 0 {
		log.Printf("kinguin retail lookup returned empty price retail=%s product=%s nominal=%s currency=%s qty=%d", retailID, productID, product.Code, quote.Currency, quote.Qty)
		return s.max.SendText(ctx, user.PlatformChatID, "Не удалось получить цену товара. Попробуйте позже.", nil)
	}
	sourceCurrency := normalizeCurrency(quote.Currency)
	sourcePriceUSD := s.sourcePriceUSD(quote.Price, sourceCurrency)
	if product.MaxKinguinPriceUSD > 0 && sourcePriceUSD > product.MaxKinguinPriceUSD {
		if err := s.repo.AddWaitlist(ctx, user.ID, product.Code, product.Label); err != nil {
			log.Printf("add waitlist user=%d nominal=%s: %v", user.ID, product.Code, err)
		}
		log.Printf("kinguin source price blocked nominal=%s product=%s price=%.2f %s price_usd=%.2f limit_usd=%.2f", product.Code, productID, quote.Price, sourceCurrency, sourcePriceUSD, product.MaxKinguinPriceUSD)
		_ = s.notifyAdmins(ctx, "Kinguin price over limit",
			"Nominal: "+product.Label,
			"Kinguin: "+fmt.Sprintf("%.2f/%.2f USD", sourcePriceUSD, product.MaxKinguinPriceUSD),
			"Source: "+fmt.Sprintf("%.2f %s", quote.Price, sourceCurrency),
		)
		return s.max.SendText(ctx, user.PlatformChatID, "Данный номинал карточек временно закончился. Повторите попытку позднее, мы напишем вам, когда карточки появятся в продаже.", nil)
	}
	balance, err := s.repo.WalletBalance(ctx, sourceCurrency)
	if err != nil {
		log.Printf("wallet balance check failed nominal=%s currency=%s price=%.2f: %v", product.Code, sourceCurrency, quote.Price, err)
		_ = s.notifyAdmins(ctx, "Ошибка внутреннего баланса",
			"Nominal: "+product.Label,
			"User: "+user.PlatformUserID,
			"Needed: "+fmt.Sprintf("%.2f %s", quote.Price, sourceCurrency),
			"Error: "+shortError(err),
		)
		return s.max.SendText(ctx, user.PlatformChatID, "Не удалось проверить наличие карточки. Повторите попытку позднее.", nil)
	}
	if balance < quote.Price {
		if err := s.repo.AddWaitlist(ctx, user.ID, product.Code, product.Label); err != nil {
			log.Printf("add waitlist user=%d nominal=%s: %v", user.ID, product.Code, err)
		}
		_ = s.notifyAdmins(ctx, "Не хватает внутреннего баланса Kinguin",
			"Nominal: "+product.Label,
			"User: "+user.PlatformUserID,
			"Needed: "+fmt.Sprintf("%.2f %s", quote.Price, sourceCurrency),
			"Balance: "+fmt.Sprintf("%.2f %s", balance, sourceCurrency),
		)
		return s.max.SendText(ctx, user.PlatformChatID, "Данный номинал карточек временно закончился. Повторите попытку позднее, мы напишем вам, когда карточки появятся в продаже.", nil)
	}
	_ = s.notifyWaitlistRestocked(ctx, product.Code, product.Label)
	orderSum := s.calculateRUB(quote.Price, sourceCurrency)
	order, err := s.repo.CreateOrder(ctx, models.Order{
		UserID:           user.ID,
		NominalCode:      product.Code,
		ProductLabel:     product.Label,
		KinguinProductID: productID,
		SourcePrice:      quote.Price,
		SourceCurrency:   sourceCurrency,
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
	_ = s.recordUserEvent(ctx, user, "payment_created", product.Code)
	_ = s.notifyFunnelEvent(ctx, user, "Payment created", product.Code, fmt.Sprintf("%.0f руб.", orderSum))
	return s.max.SendText(ctx, user.PlatformChatID,
		fmt.Sprintf("Счет на оплату: %s\nСумма к оплате: %.0f руб.\n\nПосле оплаты мы проверим заказ и выдадим код.", product.Label, orderSum),
		[][]maxapi.Button{{{Text: fmt.Sprintf("Оплатить %.0f руб.", orderSum), URL: payment.URL}}})
}

func (s *ShopService) calculateRUB(price float64, currency string) float64 {
	base := s.sourceCostRUB(price, currency)
	margin := s.cfg.FixedFeeRUB
	if s.cfg.MarkupPercent > 0 {
		percentMargin := base * s.cfg.MarkupPercent / 100
		if s.cfg.DynamicMarginRUB > 0 && percentMargin > s.cfg.DynamicMarginRUB {
			percentMargin = s.cfg.DynamicMarginRUB
		}
		margin += percentMargin
	} else {
		margin += s.cfg.DynamicMarginRUB
	}
	sum := base + margin
	if s.cfg.AcquiringFeePercent > 0 && s.cfg.AcquiringFeePercent < 100 {
		sum = sum / (1 - s.cfg.AcquiringFeePercent/100)
	}
	return roundUpToNine(sum)
}

func (s *ShopService) sourceCostRUB(price float64, currency string) float64 {
	rate := s.cfg.USDRUBRate
	switch strings.ToUpper(currency) {
	case "EUR":
		rate = s.cfg.EURRUBRate
	case "RUB":
		rate = 1
	}
	return price * rate
}

func (s *ShopService) sourcePriceUSD(price float64, currency string) float64 {
	switch strings.ToUpper(currency) {
	case "EUR":
		if s.cfg.USDRUBRate <= 0 {
			return price
		}
		return price * s.cfg.EURRUBRate / s.cfg.USDRUBRate
	case "RUB":
		if s.cfg.USDRUBRate <= 0 {
			return price
		}
		return price / s.cfg.USDRUBRate
	default:
		return price
	}
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

func (s *ShopService) sendAdminMenu(ctx context.Context, chatID string) error {
	return s.max.SendText(ctx, chatID, strings.Join([]string{
		"Админ-меню",
		"",
		"/adstats метка - статистика по метке",
		"/adstats_all - статистика по всем меткам",
		"/botstats - общая статистика",
		"/choicestats - статистика выбора номинала",
		"/adtag метка - создать ссылку с меткой",
		"/push текст - отправить пуш активным пользователям и админам",
		"/push_stats - диагностика базы и последнего пуша",
		"/payments - последние оплаты",
		"/errors - последние ошибки",
		"/balance - внутренний баланс Kinguin",
	}, "\n"), nil)
}

func (s *ShopService) sendBotStats(ctx context.Context, chatID string) error {
	stats, err := s.repo.Stats(ctx)
	if err != nil {
		return err
	}
	events, err := s.repo.EventStats(ctx, "")
	if err != nil {
		return err
	}
	lines := []string{"Общая статистика", "", "Заказы:"}
	for _, status := range []string{models.OrderStatusCreated, models.OrderStatusPending, models.OrderStatusPaid, models.OrderStatusSuccess, models.OrderStatusError, models.OrderStatusManual} {
		lines = append(lines, status+": "+strconv.FormatInt(stats[status], 10))
	}
	lines = append(lines, "", "События:")
	lines = append(lines, formatEventStats(events)...)
	return s.max.SendText(ctx, chatID, strings.Join(lines, "\n"), nil)
}

func (s *ShopService) sendAdStats(ctx context.Context, chatID, tag string) error {
	events, err := s.repo.EventStats(ctx, tag)
	if err != nil {
		return err
	}
	title := "Статистика по всем меткам"
	if strings.TrimSpace(tag) != "" {
		title = "Статистика по метке: " + strings.TrimSpace(tag)
	}
	lines := []string{title}
	lines = append(lines, formatEventStats(events)...)
	return s.max.SendText(ctx, chatID, strings.Join(lines, "\n"), nil)
}

func (s *ShopService) sendChoiceStats(ctx context.Context, chatID string) error {
	stats, err := s.repo.ChoiceStats(ctx)
	if err != nil {
		return err
	}
	lines := []string{"Статистика выбора номинала"}
	lines = append(lines, formatEventStats(stats)...)
	return s.max.SendText(ctx, chatID, strings.Join(lines, "\n"), nil)
}

func (s *ShopService) createAdTag(ctx context.Context, chatID, tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return s.max.SendText(ctx, chatID, "Формат: /adtag link2", nil)
	}
	link := s.cfg.ReturnToBotURL
	if strings.Contains(link, "?") {
		link += "&start=" + url.QueryEscape(tag)
	} else {
		link += "?start=" + url.QueryEscape(tag)
	}
	return s.max.SendText(ctx, chatID, "Ссылка для метки "+tag+":\n"+link, nil)
}

func (s *ShopService) sendPush(ctx context.Context, chatID, text string) error {
	if strings.TrimSpace(text) == "" {
		return s.max.SendText(ctx, chatID, "Формат: /push текст сообщения", nil)
	}
	users, err := s.repo.ActiveUsers(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	sent, failed := 0, 0
	for _, user := range users {
		if user.PlatformChatID == "" || seen[user.PlatformChatID] {
			continue
		}
		seen[user.PlatformChatID] = true
		if err := s.max.SendText(ctx, user.PlatformChatID, text, nil); err != nil {
			failed++
			continue
		}
		sent++
	}
	for _, adminID := range s.cfg.AdminPlatformIDs {
		if adminID == "" || seen[adminID] {
			continue
		}
		seen[adminID] = true
		if err := s.max.SendText(ctx, adminID, text, nil); err != nil {
			failed++
			continue
		}
		sent++
	}
	_ = s.repo.LogPush(ctx, text, sent, failed)
	return s.max.SendText(ctx, chatID, fmt.Sprintf("Пуш отправлен.\nSent: %d\nErrors: %d", sent, failed), nil)
}

func (s *ShopService) sendPushStats(ctx context.Context, chatID string) error {
	stats, err := s.repo.PushStats(ctx)
	if err != nil {
		return err
	}
	return s.max.SendText(ctx, chatID, stats, nil)
}

func (s *ShopService) sendRecentPayments(ctx context.Context, chatID string) error {
	lines, err := s.repo.RecentPayments(ctx, 10)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		lines = []string{"Оплат пока нет."}
	}
	return s.max.SendText(ctx, chatID, "Последние оплаты:\n"+strings.Join(lines, "\n"), nil)
}

func (s *ShopService) sendRecentErrors(ctx context.Context, chatID string) error {
	lines, err := s.repo.RecentErrors(ctx, 10)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		lines = []string{"Ошибок пока нет."}
	}
	return s.max.SendText(ctx, chatID, "Последние ошибки:\n"+strings.Join(lines, "\n"), nil)
}

func (s *ShopService) sendWalletBalance(ctx context.Context, chatID string) error {
	lines := []string{"Внутренний баланс Kinguin:"}
	for _, currency := range []string{"EUR", "USD"} {
		amount, err := s.repo.WalletBalance(ctx, currency)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s: %.2f", currency, amount))
	}
	lines = append(lines, "", "Команды:", "указать баланс 12.50", "пополнить баланс 5")
	return s.max.SendText(ctx, chatID, strings.Join(lines, "\n"), nil)
}

func (s *ShopService) setWalletBalance(ctx context.Context, chatID, text string) error {
	amount, currency, err := parseWalletAmount(text, []string{"/setbalance", "указать баланс"})
	if err != nil {
		return s.max.SendText(ctx, chatID, "Формат: указать баланс 12.50 или /setbalance 12.50 EUR", nil)
	}
	current, err := s.repo.SetWalletBalance(ctx, currency, amount)
	if err != nil {
		return err
	}
	_ = s.notifyRestocksForCurrency(ctx, currency)
	return s.max.SendText(ctx, chatID, fmt.Sprintf("Баланс Kinguin установлен: %.2f %s", current, currency), nil)
}

func (s *ShopService) addWalletBalance(ctx context.Context, chatID, text string) error {
	amount, currency, err := parseWalletAmount(text, []string{"/addbalance", "пополнить баланс"})
	if err != nil {
		return s.max.SendText(ctx, chatID, "Формат: пополнить баланс 5 или /addbalance 5 EUR", nil)
	}
	current, err := s.repo.AddWalletBalance(ctx, currency, amount)
	if err != nil {
		return err
	}
	_ = s.notifyRestocksForCurrency(ctx, currency)
	return s.max.SendText(ctx, chatID, fmt.Sprintf("Баланс Kinguin пополнен на %.2f %s. Сейчас: %.2f %s", amount, currency, current, currency), nil)
}

func (s *ShopService) notifyRestocksForCurrency(ctx context.Context, currency string) error {
	for _, product := range s.cfg.Products {
		quote, err := s.kinguin.ResolveRetailProduct(ctx, product.KinguinRetailID)
		if err != nil || quote.Price <= 0 || quote.Qty <= 0 || normalizeCurrency(quote.Currency) != currency {
			continue
		}
		balance, err := s.repo.WalletBalance(ctx, currency)
		if err != nil || balance < quote.Price {
			continue
		}
		if err := s.notifyWaitlistRestocked(ctx, product.Code, product.Label); err != nil {
			log.Printf("notify restock nominal=%s: %v", product.Code, err)
		}
	}
	return nil
}

func (s *ShopService) markOrderSuccessAndDebit(ctx context.Context, order *models.Order, kinguinOrderID, code string) error {
	if err := s.repo.MarkOrderSuccess(ctx, order.ID, kinguinOrderID, code); err != nil {
		return err
	}
	if order.SourcePrice <= 0 || strings.TrimSpace(order.SourceCurrency) == "" {
		return nil
	}
	currency := normalizeCurrency(order.SourceCurrency)
	balance, debited, err := s.repo.DebitWalletForOrder(ctx, order.ID, currency, order.SourcePrice)
	if err != nil {
		log.Printf("wallet debit failed order=%d amount=%.2f %s: %v", order.ID, order.SourcePrice, currency, err)
		_ = s.notifyAdmins(ctx, "Не списался внутренний баланс",
			"Order: "+fmt.Sprint(order.ID),
			"Amount: "+fmt.Sprintf("%.2f %s", order.SourcePrice, currency),
			"Error: "+shortError(err),
		)
		return nil
	}
	if debited {
		log.Printf("wallet debited order=%d amount=%.2f %s balance=%.2f", order.ID, order.SourcePrice, currency, balance)
	}
	return nil
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
		log.Printf("manual deliver code not found order=%d kinguin_order=%s details=%s", order.ID, kinguinOrderID, details)
		return s.max.SendText(ctx, adminChatID, "Код пока не найден.\nOrder: "+fmt.Sprint(order.ID)+"\nKinguin order: "+kinguinOrderID, nil)
	}
	if err := s.markOrderSuccessAndDebit(ctx, order, kinguinOrderID, code); err != nil {
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

func (s *ShopService) recordUserEvent(ctx context.Context, user *models.User, eventType, details string) error {
	if err := s.repo.RecordEvent(ctx, user, eventType, details); err != nil {
		log.Printf("record event user=%s event=%s: %v", user.PlatformUserID, eventType, err)
		return err
	}
	return nil
}

func (s *ShopService) notifyFunnelEvent(ctx context.Context, user *models.User, title, reason, eventType string) error {
	if user == nil {
		return nil
	}
	return s.notifyAdmins(ctx,
		title,
		"ID: "+user.PlatformUserID,
		"Tag: "+emptyDefault(user.AdTag, "direct"),
		"Reason: "+reason,
		"Type: "+eventType,
	)
}

func formatEventStats(items []models.EventStat) []string {
	if len(items) == 0 {
		return []string{"Нет данных."}
	}
	lines := []string{}
	for _, item := range items {
		lines = append(lines, item.Name+": "+strconv.FormatInt(item.Count, 10))
	}
	return lines
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

func safeKinguinProduct(quote models.ProductQuote) (bool, string) {
	name := strings.ToLower(strings.TrimSpace(quote.Name))
	description := strings.ToLower(strings.TrimSpace(quote.Description))
	combined := name + " " + description
	itemType := strings.ToUpper(strings.TrimSpace(quote.ItemType))
	region := strings.ToLower(strings.TrimSpace(quote.Region))

	if itemType == "TOPUP" {
		return false, "topup item type"
	}
	for _, phrase := range []string{"top-up", "top up", "direct topup", "direct top-up", "account"} {
		if strings.Contains(combined, phrase) {
			return false, "blocked phrase: " + phrase
		}
	}
	for _, code := range []string{"us", "usa", "eu", "europe", "uk", "united kingdom", "de", "fr", "nz", "au", "ca", "gb"} {
		if hasRegionWord(combined, code) {
			return false, "blocked region: " + code
		}
	}

	if itemType != "" && itemType != "KEY" && itemType != "ECARD" {
		return false, "unsupported item type: " + itemType
	}
	isGlobal := strings.Contains(region, "global") ||
		strings.Contains(region, "free") ||
		strings.Contains(region, "row") ||
		strings.Contains(name, "global") ||
		strings.Contains(name, "region free") ||
		strings.Contains(name, "region-free")
	if !isGlobal {
		return false, "region is not global"
	}
	return true, ""
}

func hasRegionWord(text, word string) bool {
	pattern := `(?i)\b` + regexp.QuoteMeta(word) + `\b`
	return regexp.MustCompile(pattern).MatchString(text)
}

func parseWalletAmount(text string, prefixes []string) (float64, string, error) {
	lower := strings.ToLower(strings.TrimSpace(text))
	rest := strings.TrimSpace(text)
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			rest = strings.TrimSpace(text[len(prefix):])
			break
		}
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, "", fmt.Errorf("amount is empty")
	}
	rawAmount := strings.NewReplacer(",", ".", "€", "", "$", "").Replace(fields[0])
	amount, err := strconv.ParseFloat(rawAmount, 64)
	if err != nil || amount < 0 {
		return 0, "", fmt.Errorf("invalid amount")
	}
	currency := "EUR"
	if len(fields) > 1 {
		currency = normalizeCurrency(fields[1])
	}
	return amount, currency, nil
}

func normalizeCurrency(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return "EUR"
	}
	return currency
}

func shortError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return ""
	}
	for _, sep := range []string{" | ", "\n", "{", "<html"} {
		if idx := strings.Index(text, sep); idx > 0 {
			text = strings.TrimSpace(text[:idx])
		}
	}
	if len(text) > 180 {
		text = text[:180] + "..."
	}
	return text
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
		AdTag:          emptyDefault(msg.AdTag, "direct"),
	}
}

func orderUser(order *models.Order) *models.User {
	return &models.User{
		ID:             order.UserID,
		PlatformUserID: order.PlatformUserID,
		PlatformChatID: order.PlatformChatID,
		AdTag:          "direct",
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

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func escapeHTML(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return replacer.Replace(value)
}
