package handlers

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strconv"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/payments"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/services"
)

type PaymentHandler struct {
	repo     *repositories.Repository
	yookassa *payments.YooKassa
	service  *services.ShopService
}

func NewPaymentHandler(repo *repositories.Repository, yookassa *payments.YooKassa, service *services.ShopService) *PaymentHandler {
	return &PaymentHandler{repo: repo, yookassa: yookassa, service: service}
}

func (h *PaymentHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /pay/success", h.success)
	mux.HandleFunc("POST /pay/yookassa/webhook", h.yooKassaWebhook)
}

func (h *PaymentHandler) success(w http.ResponseWriter, r *http.Request) {
	orderID, _ := strconv.ParseInt(r.URL.Query().Get("order"), 10, 64)
	if orderID <= 0 {
		h.render(w, "Заказ не найден", "Вернитесь в бот и создайте заказ заново.")
		return
	}
	order, err := h.repo.GetOrder(r.Context(), orderID)
	if err != nil {
		h.render(w, "Заказ не найден", "Если деньги списались, напишите администратору.")
		return
	}
	if order.PaymentID == "" || !h.yookassa.Enabled() {
		h.render(w, "Проверяем оплату", "Вернитесь в бот. Если оплата прошла, код придет автоматически.")
		return
	}
	payment, err := h.yookassa.Get(r.Context(), order.PaymentID)
	if err != nil {
		log.Printf("get yookassa payment order=%d payment=%s: %v", order.ID, order.PaymentID, err)
		h.render(w, "Проверяем оплату", "Платежная система пока не вернула финальный статус. Код придет в бот после подтверждения оплаты.")
		return
	}
	if payment.Status == "succeeded" || payment.Paid {
		if err := h.service.CompletePaidOrder(r.Context(), order.ID, payment.ID); err != nil {
			log.Printf("complete paid order=%d: %v", order.ID, err)
		}
		h.render(w, "Оплата прошла", "Заказ принят. Вернитесь в бот, там будет статус выдачи кода.")
		return
	}
	h.render(w, "Оплата еще не завершена", "Текущий статус: "+payment.Status+". Завершите платеж и вернитесь в бот.")
}

func (h *PaymentHandler) yooKassaWebhook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Event  string `json:"event"`
		Object struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Paid   bool   `json:"paid"`
			Metadata struct {
				OrderID string `json:"order_id"`
			} `json:"metadata"`
		} `json:"object"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.Object.ID == "" || in.Object.Metadata.OrderID == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	orderID, err := strconv.ParseInt(in.Object.Metadata.OrderID, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	log.Printf("yookassa webhook event=%s order=%d payment=%s status=%s paid=%v", in.Event, orderID, in.Object.ID, in.Object.Status, in.Object.Paid)
	if in.Object.Status == "succeeded" || in.Object.Paid {
		if err := h.service.CompletePaidOrder(r.Context(), orderID, in.Object.ID); err != nil {
			log.Printf("complete paid order=%d payment=%s: %v", orderID, in.Object.ID, err)
			http.Error(w, "temporary error", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (h *PaymentHandler) render(w http.ResponseWriter, title, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = paymentTemplate.Execute(w, map[string]string{
		"Title": title,
		"Text":  text,
	})
}

var paymentTemplate = template.Must(template.New("payment").Parse(`<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f4f6f8; color: #18202a; font-family: system-ui, -apple-system, "Segoe UI", sans-serif; }
    main { width: min(100% - 32px, 520px); padding: 28px; background: #fff; border: 1px solid #d7dde5; border-radius: 8px; }
    h1 { margin: 0 0 12px; font-size: 26px; }
    p { margin: 0; line-height: 1.45; color: #516070; }
  </style>
</head>
<body><main><h1>{{.Title}}</h1><p>{{.Text}}</p></main></body>
</html>`))
