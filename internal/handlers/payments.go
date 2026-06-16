package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
)

type premiumPlan struct {
	Code       string
	Amount     string
	PeriodDays int
	Label      string
}

var premiumPlans = map[string]premiumPlan{
	"3d":   {Code: "3d", Amount: "49.00", PeriodDays: 3, Label: "49 ₽ / 3 дня"},
	"week": {Code: "week", Amount: "199.00", PeriodDays: 7, Label: "199 ₽ / неделя"},
}

type PaymentHandler struct {
	cfg  config.Config
	repo *repositories.Repository
	max  *maxapi.Client
	http *http.Client
}

func NewPaymentHandler(cfg config.Config, repo *repositories.Repository, max *maxapi.Client) *PaymentHandler {
	return &PaymentHandler{
		cfg:  cfg,
		repo: repo,
		max:  max,
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

func (h *PaymentHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /offer", h.offer)
	mux.HandleFunc("GET /pay", h.pay)
	mux.HandleFunc("GET /pay/success", h.success)
	mux.HandleFunc("POST /pay/yookassa/webhook", h.yooKassaWebhook)
	mux.HandleFunc("GET /matches/video", h.matchVideo)
	mux.HandleFunc("GET /matches/hide", h.hideMatch)
}

func (h *PaymentHandler) offer(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = offerTemplate.Execute(w, map[string]string{
		"Price": "49 рублей за 3 дня или 199 рублей за неделю",
		"BotURL": h.cfg.ReturnToBotURL,
	})
}

func (h *PaymentHandler) pay(w http.ResponseWriter, r *http.Request) {
	platformUserID := strings.TrimSpace(r.URL.Query().Get("u"))
	plan := premiumPlanFromRequest(r)
	user, err := h.repo.GetUserByPlatformID(r.Context(), platformUserID)
	if err != nil {
		h.renderPayMessage(w, "Пользователь не найден", "Вернитесь в бот и нажмите кнопку оплаты ещё раз.")
		return
	}
	if user.IsPremium {
		h.renderPayMessage(w, "Premium уже активен", "Вернитесь в бот и продолжайте знакомиться.")
		return
	}
	if h.cfg.YooKassaShopID == "" || h.cfg.YooKassaSecretKey == "" {
		h.renderPayMessage(w, "Касса не настроена", "Добавьте на сервер переменные YOOKASSA_SHOP_ID и YOOKASSA_SECRET_KEY, затем повторите оплату.")
		return
	}

	payment, err := h.createYooKassaPayment(r.Context(), user.ID, user.PlatformUserID, plan)
	if err != nil {
		log.Printf("create yookassa payment user=%s plan=%s amount=%s: %v", user.PlatformUserID, plan.Code, plan.Amount, err)
		h.renderPayMessage(w, "Оплата временно недоступна", "Не удалось создать платёж. Попробуйте позже.")
		return
	}
	if err := h.repo.CreatePremiumPayment(r.Context(), user.ID, plan.Amount, "yookassa", payment.Status, payment.ID, plan.Code, plan.PeriodDays, payment.PaymentMethod.ID, "initial"); err != nil {
		h.renderPayMessage(w, "Оплата временно недоступна", "Не удалось сохранить платёж. Попробуйте позже.")
		return
	}
	http.Redirect(w, r, payment.Confirmation.ConfirmationURL, http.StatusFound)
}

func (h *PaymentHandler) success(w http.ResponseWriter, r *http.Request) {
	platformUserID := strings.TrimSpace(r.URL.Query().Get("u"))
	user, err := h.repo.GetUserByPlatformID(r.Context(), platformUserID)
	if err != nil {
		h.renderPayMessage(w, "Пользователь не найден", "Вернитесь в бот и нажмите кнопку оплаты ещё раз.")
		return
	}
	storedPayment, err := h.repo.LatestPremiumPayment(r.Context(), user.ID)
	if err != nil {
		h.renderPayMessage(w, "Платёж не найден", "Если деньги списались, напишите администратору и приложите чек.")
		return
	}
	payment, err := h.getYooKassaPayment(r.Context(), storedPayment.ExternalID)
	if err != nil {
		h.renderPayMessage(w, "Проверяем оплату", "Касса пока не вернула результат. Подождите минуту и откройте эту страницу снова.")
		return
	}
	if err := h.repo.UpdatePremiumPaymentStatus(r.Context(), storedPayment.ExternalID, payment.Status, payment.PaymentMethod.ID); err != nil {
		h.renderPayMessage(w, "Ошибка сохранения", "Оплата найдена, но статус не сохранился. Напишите администратору.")
		return
	}
	if payment.Status != "succeeded" {
		h.renderPayMessage(w, "Оплата ещё не завершена", "Текущий статус: "+payment.Status+". Вернитесь на страницу оплаты и завершите платёж.")
		return
	}
	plan := premiumPlanByCode(storedPayment.Plan)
	paymentMethodID := firstNonEmptyPayment(payment.PaymentMethod.ID, storedPayment.PaymentMethodID)
	until := time.Now().AddDate(0, 0, plan.PeriodDays)
	if err := h.repo.SetPremiumSubscription(r.Context(), user.ID, plan.Code, plan.Amount, plan.PeriodDays, paymentMethodID, until); err != nil {
		h.renderPayMessage(w, "Premium оплачен", "Оплата прошла, но доступ не включился автоматически. Напишите администратору.")
		return
	}
	if user.PremiumOfferMessageID != "" {
		_ = h.max.DeleteMessage(r.Context(), user.PremiumOfferChatID, user.PremiumOfferMessageID)
		_ = h.repo.ClearPremiumOfferMessage(r.Context(), user.ID)
	}
	if target, err := h.repo.LatestContactRequest(r.Context(), user.ID); err == nil {
		_ = h.sendPremiumContact(r.Context(), *user, *target)
	}
	h.renderPayMessage(w, "Premium активирован", "Подписка активна до "+until.Format("02.01.2006 15:04")+". Сейчас вернём вас в бот.", true)
}

func (h *PaymentHandler) yooKassaWebhook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Event  string          `json:"event"`
		Object yooKassaPayment `json:"object"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.Object.ID == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := h.applyYooKassaPayment(r.Context(), in.Object); err != nil {
		log.Printf("apply yookassa webhook payment=%s event=%s: %v", in.Object.ID, in.Event, err)
		http.Error(w, "temporary error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *PaymentHandler) applyYooKassaPayment(ctx context.Context, payment yooKassaPayment) error {
	stored, userID, err := h.repo.GetPremiumPaymentByExternalID(ctx, payment.ID)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil
		}
		return err
	}
	paymentMethodID := firstNonEmptyPayment(payment.PaymentMethod.ID, stored.PaymentMethodID)
	if err := h.repo.UpdatePremiumPaymentStatus(ctx, payment.ID, payment.Status, paymentMethodID); err != nil {
		return err
	}
	if payment.Status != "succeeded" {
		return nil
	}
	plan := premiumPlanByCode(stored.Plan)
	until := time.Now().AddDate(0, 0, plan.PeriodDays)
	if err := h.repo.SetPremiumSubscription(ctx, userID, plan.Code, plan.Amount, plan.PeriodDays, paymentMethodID, until); err != nil {
		return err
	}
	user, err := h.repo.GetUserByID(ctx, userID)
	if err == nil {
		_ = h.max.SendText(ctx, user.PlatformChatID, "💎 Premium активирован.\n\nПодписка активна до "+until.Format("02.01.2006 15:04")+".", [][]maxapi.Button{
			{{Text: "▶️ Продолжить просмотр", Payload: "browse"}},
			{{Text: "☰ Главное меню", Payload: "main_menu"}},
		})
	}
	return nil
}

func (h *PaymentHandler) StartAutorenew(ctx context.Context) {
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				h.renewDueSubscriptions(ctx)
				timer.Reset(30 * time.Minute)
			}
		}
	}()
}

func (h *PaymentHandler) renewDueSubscriptions(ctx context.Context) {
	if h.cfg.YooKassaShopID == "" || h.cfg.YooKassaSecretKey == "" {
		return
	}
	subs, err := h.repo.ListDuePremiumSubscriptions(ctx, 25)
	if err != nil {
		log.Printf("list due premium subscriptions: %v", err)
		return
	}
	for _, sub := range subs {
		key := fmt.Sprintf("renew-%d-%s-%d", sub.User.ID, sub.Plan, sub.NextChargeAt.Unix())
		payment, err := h.createYooKassaRecurringPayment(ctx, sub, key)
		if err != nil {
			log.Printf("create recurring payment user=%d: %v", sub.User.ID, err)
			_ = h.repo.DisablePremiumSubscription(ctx, sub.User.ID)
			_ = h.max.SendText(ctx, sub.User.PlatformChatID, "Не удалось продлить Premium: автосписание не прошло. Подписка отключена, ее можно подключить заново в меню.", [][]maxapi.Button{
				{{Text: "💎 Подписка", Payload: "subscription"}},
				{{Text: "☰ Главное меню", Payload: "main_menu"}},
			})
			continue
		}
		if err := h.repo.CreatePremiumPayment(ctx, sub.User.ID, sub.Amount, "yookassa", payment.Status, payment.ID, sub.Plan, sub.PeriodDays, firstNonEmptyPayment(payment.PaymentMethod.ID, sub.PaymentMethodID), "renewal"); err != nil {
			log.Printf("save recurring payment user=%d payment=%s: %v", sub.User.ID, payment.ID, err)
			continue
		}
		if payment.Status == "succeeded" {
			until := time.Now().AddDate(0, 0, sub.PeriodDays)
			if err := h.repo.SetPremiumSubscription(ctx, sub.User.ID, sub.Plan, sub.Amount, sub.PeriodDays, firstNonEmptyPayment(payment.PaymentMethod.ID, sub.PaymentMethodID), until); err != nil {
				log.Printf("extend premium user=%d payment=%s: %v", sub.User.ID, payment.ID, err)
				continue
			}
			_ = h.max.SendText(ctx, sub.User.PlatformChatID, "💎 Подписка Premium продлена до "+until.Format("02.01.2006 15:04")+".", nil)
			continue
		}
		if payment.Status == "pending" || payment.Status == "waiting_for_capture" {
			_ = h.repo.PostponePremiumSubscription(ctx, sub.User.ID, time.Now().Add(time.Hour))
			log.Printf("recurring payment user=%d payment=%s status=%s", sub.User.ID, payment.ID, payment.Status)
			continue
		}
		_ = h.repo.DisablePremiumSubscription(ctx, sub.User.ID)
		_ = h.max.SendText(ctx, sub.User.PlatformChatID, "Premium не продлен: статус платежа "+payment.Status+". Подписка отключена, ее можно подключить заново в меню.", [][]maxapi.Button{
			{{Text: "💎 Подписка", Payload: "subscription"}},
			{{Text: "☰ Главное меню", Payload: "main_menu"}},
		})
		log.Printf("recurring payment user=%d payment=%s status=%s", sub.User.ID, payment.ID, payment.Status)
	}
}

func (h *PaymentHandler) sendPremiumContact(ctx context.Context, user models.User, target models.User) error {
	text := "💎 Premium активирован.\n\nКонтакт открыт: " + displayName(target)
	buttons := [][]maxapi.Button{}
	if link := normalizeProfileURL(target.ProfileLink); link != "" {
		buttons = append(buttons, []maxapi.Button{{Text: "💬 Написать", URL: link}})
	}
	buttons = append(buttons,
		[]maxapi.Button{{Text: "▶️ Продолжить просмотр", Payload: "browse"}},
		[]maxapi.Button{{Text: "☰ Главное меню", Payload: "main_menu"}},
	)
	return h.max.SendText(ctx, user.PlatformChatID, text, buttons)
}

func (h *PaymentHandler) hideMatch(w http.ResponseWriter, r *http.Request) {
	platformUserID := strings.TrimSpace(r.URL.Query().Get("u"))
	matchID := strings.TrimSpace(r.URL.Query().Get("m"))
	user, err := h.repo.GetUserByPlatformID(r.Context(), platformUserID)
	if err != nil {
		h.renderPayMessage(w, "Не удалось удалить", "Вернитесь в бот и попробуйте открыть список взаимных лайков заново.")
		return
	}
	otherID, err := parseInt64(matchID)
	if err != nil || otherID == 0 {
		h.renderPayMessage(w, "Не удалось удалить", "Ссылка удаления некорректна.")
		return
	}
	if err := h.repo.HideMatchForUser(r.Context(), user.ID, otherID); err != nil {
		h.renderPayMessage(w, "Не удалось удалить", "Попробуйте позже или вернитесь в бот.")
		return
	}
	_ = h.max.SendText(r.Context(), user.PlatformChatID, "Контакт удален из взаимных лайков.", [][]maxapi.Button{
		{{Text: "📬 Взаимные лайки", Payload: "matches"}},
		{{Text: "☰ Главное меню", Payload: "main_menu"}},
	})
	h.renderPayMessage(w, "Контакт удален", "Вернитесь в бот, чтобы продолжить просмотр.", true)
}

func (h *PaymentHandler) matchVideo(w http.ResponseWriter, r *http.Request) {
	platformUserID := strings.TrimSpace(r.URL.Query().Get("u"))
	matchID := strings.TrimSpace(r.URL.Query().Get("m"))
	user, err := h.repo.GetUserByPlatformID(r.Context(), platformUserID)
	if err != nil {
		h.renderPayMessage(w, "Не удалось открыть кружок", "Вернитесь в бот и попробуйте открыть список взаимных лайков заново.")
		return
	}
	otherID, err := parseInt64(matchID)
	if err != nil || otherID == 0 {
		h.renderPayMessage(w, "Не удалось открыть кружок", "Ссылка на кружок некорректна.")
		return
	}
	if _, err := h.repo.FindVisibleMatch(r.Context(), user.ID, otherID); err != nil {
		h.renderPayMessage(w, "Кружок недоступен", "Этот контакт больше недоступен в списке взаимных лайков.")
		return
	}
	other, err := h.repo.GetUserByID(r.Context(), otherID)
	if err != nil {
		h.renderPayMessage(w, "Не удалось открыть кружок", "Вернитесь в бот и попробуйте позже.")
		return
	}
	video, err := h.repo.GetActiveVideoByUser(r.Context(), otherID)
	if err != nil {
		h.renderPayMessage(w, "Кружок недоступен", "У этого контакта нет активного кружка.")
		return
	}
	messageID, err := h.max.SendMediaToDialogOrUser(r.Context(), user.PlatformDialogID, user.PlatformChatID, video.PlatformMediaID, displayName(*other), matchVideoButtons(*other))
	if err != nil {
		h.renderPayMessage(w, "Не удалось отправить кружок", "Вернитесь в бот и попробуйте позже.")
		return
	}
	go func(chatID, mid string) {
		time.Sleep(60 * time.Second)
		_ = h.max.DeleteMessage(context.Background(), chatID, mid)
	}(user.PlatformChatID, messageID)
	h.renderPayMessage(w, "Кружок отправлен", "Мы отправили кружок в чат бота.", true)
}

func displayName(user models.User) string {
	name := strings.TrimSpace(user.Name)
	if name != "" {
		return name
	}
	return "Пользователь"
}

func normalizeProfileURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if strings.HasPrefix(value, "u/") {
		return "https://max.ru/" + value
	}
	if strings.HasPrefix(value, "@") {
		return "https://max.ru/" + strings.TrimPrefix(value, "@")
	}
	return value
}

func matchVideoButtons(other models.User) [][]maxapi.Button {
	buttons := [][]maxapi.Button{}
	if link := normalizeProfileURL(other.ProfileLink); link != "" {
		buttons = append(buttons, []maxapi.Button{{Text: "💬 Написать", URL: link}})
	} else {
		buttons = append(buttons, []maxapi.Button{{Text: "💬 Ссылка недоступна", Payload: "missing_profile_link"}})
	}
	buttons = append(buttons,
		[]maxapi.Button{{Text: "▶️ Продолжить просмотр", Payload: "browse"}},
		[]maxapi.Button{
			{Text: "🚨 Пожаловаться", Payload: fmt.Sprintf("report_user:%d", other.ID)},
			{Text: "☰ Меню", Payload: "main_menu"},
		},
	)
	return buttons
}

func parseInt64(value string) (int64, error) {
	var id int64
	_, err := fmt.Sscan(strings.TrimSpace(value), &id)
	return id, err
}

func premiumPlanFromRequest(r *http.Request) premiumPlan {
	return premiumPlanByCode(r.URL.Query().Get("plan"))
}

func premiumPlanByCode(code string) premiumPlan {
	code = strings.TrimSpace(code)
	if plan, ok := premiumPlans[code]; ok {
		return plan
	}
	return premiumPlans["week"]
}

func firstNonEmptyPayment(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type yooKassaPayment struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Paid         bool   `json:"paid"`
	Confirmation struct {
		ConfirmationURL string `json:"confirmation_url"`
	} `json:"confirmation"`
	PaymentMethod struct {
		ID    string `json:"id"`
		Saved bool   `json:"saved"`
	} `json:"payment_method"`
}

func (h *PaymentHandler) createYooKassaPayment(ctx context.Context, userID int64, platformUserID string, plan premiumPlan) (*yooKassaPayment, error) {
	returnURL := strings.TrimRight(h.cfg.PublicBaseURL, "/") + "/pay/success?u=" + url.QueryEscape(platformUserID)
	body := map[string]any{
		"amount": map[string]string{
			"value": plan.Amount,
			"currency": "RUB",
		},
		"capture": true,
		"save_payment_method": true,
		"confirmation": map[string]string{
			"type": "redirect",
			"return_url": returnURL,
		},
		"description": "Premium подписка в боте знакомств: " + plan.Label,
		"metadata": map[string]string{
			"user_id":          fmt.Sprint(userID),
			"platform_user_id": platformUserID,
			"plan":             plan.Code,
			"period_days":      fmt.Sprint(plan.PeriodDays),
		},
	}
	var out yooKassaPayment
	idempotenceBase := "initial-" + fmt.Sprint(userID) + "-" + plan.Code + "-" + fmt.Sprint(time.Now().UnixNano())
	if err := h.yooKassaRequest(ctx, http.MethodPost, "/v3/payments", body, &out, idempotenceBase); err != nil {
		log.Printf("create yookassa payment with saved method failed user=%d plan=%s: %v", userID, plan.Code, err)
		delete(body, "save_payment_method")
		if retryErr := h.yooKassaRequest(ctx, http.MethodPost, "/v3/payments", body, &out, idempotenceBase+"-nosave"); retryErr != nil {
			return nil, retryErr
		}
	}
	if out.ID == "" || out.Confirmation.ConfirmationURL == "" {
		return nil, fmt.Errorf("yookassa response missing payment confirmation")
	}
	return &out, nil
}

func (h *PaymentHandler) getYooKassaPayment(ctx context.Context, paymentID string) (*yooKassaPayment, error) {
	var out yooKassaPayment
	if err := h.yooKassaRequest(ctx, http.MethodGet, "/v3/payments/"+url.PathEscape(paymentID), nil, &out, ""); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("yookassa response missing payment id")
	}
	return &out, nil
}

func (h *PaymentHandler) createYooKassaRecurringPayment(ctx context.Context, sub models.PremiumSubscription, idempotenceKey string) (*yooKassaPayment, error) {
	body := map[string]any{
		"amount": map[string]string{
			"value": sub.Amount,
			"currency": "RUB",
		},
		"capture": true,
		"payment_method_id": sub.PaymentMethodID,
		"description": "Автосписание Premium подписки: " + premiumPlanByCode(sub.Plan).Label,
		"metadata": map[string]string{
			"user_id":     fmt.Sprint(sub.User.ID),
			"plan":        sub.Plan,
			"period_days": fmt.Sprint(sub.PeriodDays),
			"reason":      "renewal",
		},
	}
	var out yooKassaPayment
	if err := h.yooKassaRequest(ctx, http.MethodPost, "/v3/payments", body, &out, idempotenceKey); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("yookassa recurring response missing payment id")
	}
	return &out, nil
}

func (h *PaymentHandler) yooKassaRequest(ctx context.Context, method, path string, in any, out any, idempotenceKey string) error {
	var reader *bytes.Reader
	if in == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(in)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.yookassa.ru"+path, reader)
	if err != nil {
		return err
	}
	req.SetBasicAuth(h.cfg.YooKassaShopID, h.cfg.YooKassaSecretKey)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		if idempotenceKey == "" {
			idempotenceKey = fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix())
		}
		req.Header.Set("Idempotence-Key", idempotenceKey)
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("yookassa status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (h *PaymentHandler) renderPayMessage(w http.ResponseWriter, title, text string, autoReturn ...bool) {
	shouldAutoReturn := len(autoReturn) > 0 && autoReturn[0]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = payMessageTemplate.Execute(w, map[string]any{
		"Title": title,
		"Text": text,
		"BotURL": h.cfg.ReturnToBotURL,
		"AutoReturn": shouldAutoReturn,
	})
}

var payMessageTemplate = template.Must(template.New("pay-message").Parse(`<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #101820; color: #fff; font-family: system-ui, -apple-system, "Segoe UI", sans-serif; }
    main { width: min(100% - 32px, 520px); padding: 28px; border-radius: 18px; background: #fff; color: #17202a; box-shadow: 0 24px 80px rgba(0,0,0,.28); }
    h1 { margin: 0 0 12px; font-size: 28px; }
    p { margin: 0 0 22px; color: #596472; line-height: 1.45; }
    a { display: flex; align-items: center; justify-content: center; min-height: 52px; border-radius: 14px; background: #1683ff; color: #fff; text-decoration: none; font-weight: 700; }
  </style>
</head>
<body>
<main><h1>{{.Title}}</h1><p>{{.Text}}</p><a href="{{.BotURL}}">Вернуться в бот</a></main>
{{if .AutoReturn}}
<script>
  setTimeout(function () {
    window.location.href = "{{.BotURL}}";
    setTimeout(function () { window.close(); }, 1200);
  }, 1500);
</script>
{{end}}
</body>
</html>`))

var offerTemplate = template.Must(template.New("offer").Parse(`<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Публичная оферта</title>
  <style>
    body { margin: 0; background: #f4f7fb; color: #17202a; font-family: system-ui, -apple-system, "Segoe UI", sans-serif; line-height: 1.55; }
    main { width: min(100% - 32px, 860px); margin: 0 auto; padding: 34px 0 56px; }
    section { background: #fff; border-radius: 18px; padding: 28px; box-shadow: 0 18px 60px rgba(31,48,70,.08); }
    h1 { margin: 0 0 18px; font-size: 30px; }
    h2 { margin: 26px 0 8px; font-size: 20px; }
    p { margin: 0 0 12px; }
    a { color: #1683ff; }
  </style>
</head>
<body>
<main>
<section>
<h1>Публичная оферта на предоставление Premium доступа</h1>
<p>Настоящий документ является предложением заключить договор на предоставление платного Premium доступа в боте знакомств.</p>
<h2>1. Предмет</h2>
<p>Пользователь получает Premium доступ к функциям бота: доступ к контактам пользователей, возможность писать первым без взаимного лайка и неограниченный просмотр кружков без стандартных ограничений сервиса.</p>
<h2>2. Стоимость и порядок оплаты</h2>
<p>Стоимость Premium доступа составляет {{.Price}} рублей. Оплата производится через платёжного провайдера. Доступ к контактам и неограниченному просмотру кружков активируется после подтверждения успешной оплаты.</p>
<h2>3. Условия использования</h2>
<p>Пользователь обязуется не публиковать незаконные материалы, спам, оскорбления, чужие персональные данные, материалы сексуального характера с участием несовершеннолетних, мошеннические предложения и иной вредоносный контент.</p>
<h2>4. Модерация и ограничения</h2>
<p>Администрация вправе ограничить доступ, скрыть анкету, удалить видео или заблокировать пользователя при нарушении правил, жалобах других пользователей, подозрении на мошенничество или угрозе безопасности сервиса.</p>
<h2>5. Возвраты</h2>
<p>Premium доступ относится к цифровой услуге. После активации доступа возврат возможен только если услуга не была предоставлена по технической причине на стороне сервиса. Для рассмотрения обращения пользователь должен предоставить данные платежа.</p>
<h2>6. Ответственность</h2>
<p>Сервис предоставляет техническую площадку для знакомств и не гарантирует взаимные лайки, ответы пользователей, встречи, отношения или иные результаты общения. Пользователь самостоятельно оценивает риски общения и передачи контактов.</p>
<h2>7. Персональные данные</h2>
<p>Сервис обрабатывает данные, необходимые для работы бота: идентификатор пользователя MAX, имя анкеты, выбранные параметры, видеоанкеты, лайки, жалобы и сведения об оплате. Данные используются для оказания услуги, модерации и поддержки.</p>
<h2>8. Изменение условий</h2>
<p>Администрация может обновлять условия оферты. Новая редакция применяется с момента публикации на этой странице.</p>
<h2>9. Принятие оферты</h2>
<p>Нажатие кнопки оплаты и совершение платежа означает полное согласие пользователя с условиями настоящей оферты.</p>
<p><a href="{{.BotURL}}">Вернуться в бот</a></p>
</section>
</main>
</body>
</html>`))
