package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
)

type PaymentHandler struct {
	cfg  config.Config
	repo *repositories.Repository
	http *http.Client
}

func NewPaymentHandler(cfg config.Config, repo *repositories.Repository) *PaymentHandler {
	return &PaymentHandler{
		cfg:  cfg,
		repo: repo,
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

func (h *PaymentHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /offer", h.offer)
	mux.HandleFunc("GET /pay", h.pay)
	mux.HandleFunc("GET /pay/success", h.success)
}

func (h *PaymentHandler) offer(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = offerTemplate.Execute(w, map[string]string{
		"Price": h.cfg.PremiumPrice,
		"BotURL": h.cfg.ReturnToBotURL,
	})
}

func (h *PaymentHandler) pay(w http.ResponseWriter, r *http.Request) {
	platformUserID := strings.TrimSpace(r.URL.Query().Get("u"))
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

	payment, err := h.createYooKassaPayment(r.Context(), user.ID, user.PlatformUserID)
	if err != nil {
		h.renderPayMessage(w, "Оплата временно недоступна", "Не удалось создать платёж. Попробуйте позже.")
		return
	}
	if err := h.repo.CreatePremiumPayment(r.Context(), user.ID, h.cfg.PremiumPrice, "yookassa", payment.Status, payment.ID); err != nil {
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
	externalID, _, err := h.repo.LatestPremiumPayment(r.Context(), user.ID)
	if err != nil {
		h.renderPayMessage(w, "Платёж не найден", "Если деньги списались, напишите администратору и приложите чек.")
		return
	}
	payment, err := h.getYooKassaPayment(r.Context(), externalID)
	if err != nil {
		h.renderPayMessage(w, "Проверяем оплату", "Касса пока не вернула результат. Подождите минуту и откройте эту страницу снова.")
		return
	}
	if err := h.repo.UpdatePremiumPaymentStatus(r.Context(), externalID, payment.Status); err != nil {
		h.renderPayMessage(w, "Ошибка сохранения", "Оплата найдена, но статус не сохранился. Напишите администратору.")
		return
	}
	if payment.Status != "succeeded" {
		h.renderPayMessage(w, "Оплата ещё не завершена", "Текущий статус: "+payment.Status+". Вернитесь на страницу оплаты и завершите платёж.")
		return
	}
	if err := h.repo.SetPremium(r.Context(), user.ID); err != nil {
		h.renderPayMessage(w, "Premium оплачен", "Оплата прошла, но доступ не включился автоматически. Напишите администратору.")
		return
	}
	h.renderPayMessage(w, "Premium активирован", "Сейчас вернём вас в бот. Если страница не закрылась автоматически, нажмите кнопку ниже.", true)
}

type yooKassaPayment struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Confirmation struct {
		ConfirmationURL string `json:"confirmation_url"`
	} `json:"confirmation"`
}

func (h *PaymentHandler) createYooKassaPayment(ctx context.Context, userID int64, platformUserID string) (*yooKassaPayment, error) {
	returnURL := strings.TrimRight(h.cfg.PublicBaseURL, "/") + "/pay/success?u=" + url.QueryEscape(platformUserID)
	body := map[string]any{
		"amount": map[string]string{
			"value": h.cfg.PremiumPrice,
			"currency": "RUB",
		},
		"capture": true,
		"confirmation": map[string]string{
			"type": "redirect",
			"return_url": returnURL,
		},
		"description": "Premium доступ в боте знакомств",
		"metadata": map[string]string{
			"user_id": fmt.Sprint(userID),
			"platform_user_id": platformUserID,
		},
	}
	var out yooKassaPayment
	if err := h.yooKassaRequest(ctx, http.MethodPost, "/v3/payments", body, &out); err != nil {
		return nil, err
	}
	if out.ID == "" || out.Confirmation.ConfirmationURL == "" {
		return nil, fmt.Errorf("yookassa response missing payment confirmation")
	}
	return &out, nil
}

func (h *PaymentHandler) getYooKassaPayment(ctx context.Context, paymentID string) (*yooKassaPayment, error) {
	var out yooKassaPayment
	if err := h.yooKassaRequest(ctx, http.MethodGet, "/v3/payments/"+url.PathEscape(paymentID), nil, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("yookassa response missing payment id")
	}
	return &out, nil
}

func (h *PaymentHandler) yooKassaRequest(ctx context.Context, method, path string, in any, out any) error {
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
		req.Header.Set("Idempotence-Key", fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix()))
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("yookassa status %d", resp.StatusCode)
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
