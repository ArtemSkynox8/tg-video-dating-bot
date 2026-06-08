package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/services"
)

type WebhookHandler struct {
	cfg     config.Config
	service *services.DatingService
}

func NewWebhookHandler(cfg config.Config, service *services.DatingService) *WebhookHandler {
	return &WebhookHandler{cfg: cfg, service: service}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	secret := r.Header.Get("X-Max-Bot-Api-Secret")
	if secret == "" {
		secret = r.Header.Get("X-Webhook-Secret")
	}
	if h.cfg.MaxWebhookSecret != "" && secret != h.cfg.MaxWebhookSecret {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var update maxapi.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "bad update", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var err error
	switch {
	case update.Message != nil:
		err = h.service.HandleMessage(ctx, *update.Message)
	case update.Callback != nil:
		err = h.service.HandleCallback(ctx, *update.Callback)
	default:
		log.Printf("ignored update without supported payload: %s", update.UpdateID)
	}

	if err != nil {
		log.Printf("handle update %s: %v", update.UpdateID, err)
		http.Error(w, "handler error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
