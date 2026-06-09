package handlers

import (
	"encoding/json"
	"fmt"
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
	switch update.UpdateType {
	case "message_created":
		if update.Message != nil {
			msg := normalizeMessage(update)
			log.Printf("max update message_created user=%s chat=%s sender=%s recipient_chat=%s recipient_user=%s text=%q",
				msg.From.ID, msg.Chat.ID, update.Message.Sender.ID, update.Message.Recipient.ChatID, update.Message.Recipient.UserID, msg.Text)
			err = h.service.HandleMessage(ctx, msg)
		}
	case "bot_started":
		if update.User != nil {
			err = h.service.HandleMessage(ctx, maxapi.MessageUpdate{
				MessageID: fmt.Sprintf("bot-started-%d", update.Timestamp),
				Chat:      maxapi.Chat{ID: fmt.Sprint(update.ChatID)},
				From:      *update.User,
				Text:      "/start",
			})
		}
	case "message_callback":
		if update.Callback != nil {
			cb := normalizeCallback(update)
			log.Printf("max update message_callback user=%s chat=%s payload=%q callback_user=%s recipient_chat=%s recipient_user=%s",
				cb.From.ID, cb.Chat.ID, cb.Payload, update.Callback.User.ID, update.Callback.Message.Recipient.ChatID, update.Callback.Message.Recipient.UserID)
			err = h.service.HandleCallback(ctx, cb)
		}
	default:
		log.Printf("ignored max update type=%s id=%s", update.UpdateType, update.UpdateID)
	}

	if err != nil {
		log.Printf("handle update %s: %v", update.UpdateID, err)
		http.Error(w, "handler error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func normalizeMessage(update maxapi.Update) maxapi.MessageUpdate {
	message := *update.Message
	chatID := message.Recipient.ChatID
	if chatID == "" {
		chatID = message.Recipient.UserID
	}
	from := message.Sender
	if update.User != nil && update.User.ID != "" {
		from = *update.User
	}
	return maxapi.MessageUpdate{
		MessageID: message.Body.MID,
		Chat:      maxapi.Chat{ID: chatID},
		From:      from,
		Text:      message.Body.Text,
		Media:     normalizeMedia(message.Body.Attachments),
	}
}

func normalizeCallback(update maxapi.Update) maxapi.CallbackUpdate {
	callback := *update.Callback
	chatID := callback.Message.Recipient.ChatID
	if chatID == "" {
		chatID = callback.Message.Recipient.UserID
	}
	from := callback.User
	if update.User != nil && update.User.ID != "" {
		from = *update.User
	}
	return maxapi.CallbackUpdate{
		CallbackID: callback.CallbackID,
		MessageID:  callback.Message.Body.MID,
		Chat:       maxapi.Chat{ID: chatID},
		From:       from,
		Payload:    callback.Payload,
	}
}

func normalizeMedia(attachments []maxapi.Attachment) []maxapi.Media {
	media := make([]maxapi.Media, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.Type != "video" && attachment.Type != "file" {
			continue
		}
		id := ""
		for _, key := range []string{"token", "file_id", "id", "video_token"} {
			if token, ok := attachment.Payload[key]; ok {
				id = fmt.Sprint(token)
				break
			}
		}
		if id == "" {
			continue
		}
		media = append(media, maxapi.Media{
			ID:   id,
			Type: attachment.Type,
		})
	}
	return media
}
