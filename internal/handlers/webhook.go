package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/services"
)

type WebhookHandler struct {
	cfg     config.Config
	service *services.ShopService
}

func NewWebhookHandler(cfg config.Config, service *services.ShopService) *WebhookHandler {
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

	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad update", http.StatusBadRequest)
		return
	}

	var update maxapi.Update
	if err := json.Unmarshal(rawBody, &update); err != nil {
		http.Error(w, "bad update", http.StatusBadRequest)
		return
	}

	switch update.UpdateType {
	case "message_created":
		if update.Message != nil {
			err = h.service.HandleMessage(r.Context(), normalizeMessage(update))
		}
	case "bot_started":
		if update.User != nil {
			err = h.service.HandleMessage(r.Context(), maxapi.MessageUpdate{
				MessageID: fmt.Sprintf("bot-started-%d", update.Timestamp),
				Chat:      maxapi.Chat{ID: update.User.ID},
				From:      *update.User,
				Text:      "/start",
				AdTag:     firstNonEmpty(update.Payload, update.StartParam, update.StartParamCamel, update.StartPayload, update.StartPayloadCamel, update.DeepLinkPayload),
			})
		}
	case "message_callback":
		if update.Callback != nil {
			err = h.service.HandleCallback(r.Context(), normalizeCallback(update))
		}
	default:
		log.Printf("ignored max update type=%s id=%s", update.UpdateType, update.UpdateID)
	}

	if err != nil {
		log.Printf("handle max update %s: %v", update.UpdateID, err)
		http.Error(w, "handler error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeMessage(update maxapi.Update) maxapi.MessageUpdate {
	message := *update.Message
	from := message.Sender
	if update.User != nil && update.User.ID != "" {
		from = *update.User
	}
	chatID := from.ID
	if chatID == "" {
		chatID = message.Recipient.ChatID
	}
	if chatID == "" {
		chatID = message.Recipient.UserID
	}
	return maxapi.MessageUpdate{
		MessageID: message.Body.MID,
		Chat:      maxapi.Chat{ID: chatID},
		Dialog:    maxapi.Chat{ID: message.Recipient.ChatID},
		From:      from,
		Text:      strings.TrimSpace(message.Body.Text),
	}
}

func normalizeCallback(update maxapi.Update) maxapi.CallbackUpdate {
	callback := *update.Callback
	from := callback.User
	if update.User != nil && update.User.ID != "" {
		from = *update.User
	}
	chatID := from.ID
	if chatID == "" {
		chatID = callback.Message.Recipient.ChatID
	}
	if chatID == "" {
		chatID = callback.Message.Recipient.UserID
	}
	return maxapi.CallbackUpdate{
		CallbackID: callback.CallbackID,
		MessageID:  callback.Message.Body.MID,
		Chat:       maxapi.Chat{ID: chatID},
		Dialog:     maxapi.Chat{ID: callback.Message.Recipient.ChatID},
		From:       from,
		Payload:    callback.Payload,
	}
}
