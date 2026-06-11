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

	rawBody, readErr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if readErr != nil {
		http.Error(w, "bad update", http.StatusBadRequest)
		return
	}

	var update maxapi.Update
	if err := json.Unmarshal(rawBody, &update); err != nil {
		http.Error(w, "bad update", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var err error
	switch update.UpdateType {
	case "message_created":
		if update.Message != nil {
			msg := normalizeMessage(update)
			log.Printf("max update message_created user=%s chat=%s sender=%s recipient_chat=%s recipient_user=%s profile_link=%q attachments=%d media=%d contacts=%d text=%q",
				msg.From.ID, msg.Chat.ID, update.Message.Sender.ID, update.Message.Recipient.ChatID, update.Message.Recipient.UserID, msg.From.ProfileLink, len(update.Message.Body.Attachments), len(msg.Media), len(msg.Contacts), msg.Text)
			if shouldLogRawMessage(update.Message) {
				log.Printf("max raw message=%s", limitLog(string(rawBody), 4096))
			}
			err = h.service.HandleMessage(ctx, msg)
		}
	case "bot_started":
		if update.User != nil {
			err = h.service.HandleMessage(ctx, maxapi.MessageUpdate{
				MessageID: fmt.Sprintf("bot-started-%d", update.Timestamp),
				Chat:      maxapi.Chat{ID: update.User.ID},
				From:      *update.User,
				Text:      "/start",
			})
		}
	case "message_callback":
		if update.Callback != nil {
			cb := normalizeCallback(update)
			log.Printf("max update message_callback user=%s chat=%s payload=%q callback_user=%s recipient_chat=%s recipient_user=%s profile_link=%q",
				cb.From.ID, cb.Chat.ID, cb.Payload, update.Callback.User.ID, update.Callback.Message.Recipient.ChatID, update.Callback.Message.Recipient.UserID, cb.From.ProfileLink)
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
		Text:      message.Body.Text,
		Media:     normalizeMedia(message.Body.Attachments),
		Contacts:  normalizeContacts(message.Body.Attachments),
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

func normalizeMedia(attachments []maxapi.Attachment) []maxapi.Media {
	media := make([]maxapi.Media, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.Type != "video" && attachment.Type != "file" {
			continue
		}
		id := findPayloadValue(attachment.Payload, []string{"token", "file_id", "video_token", "id"})
		if id == "" {
			continue
		}
		media = append(media, maxapi.Media{
			ID:       id,
			Type:     attachment.Type,
			URL:      findPayloadValue(attachment.Payload, []string{"url", "download_url", "downloadUrl"}),
			Duration: parsePayloadInt(attachment.Payload, []string{"duration", "duration_ms", "durationMs"}),
		})
	}
	return media
}

func normalizeContacts(attachments []maxapi.Attachment) []maxapi.Contact {
	contacts := make([]maxapi.Contact, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.Type != "contact" {
			continue
		}
		contacts = append(contacts, maxapi.Contact{
			Name:   findPayloadValue(attachment.Payload, []string{"name", "full_name", "fullName", "first_name", "firstName"}),
			Phone:  findPayloadValue(attachment.Payload, []string{"phone", "phone_number", "phoneNumber", "phone_number_normalized"}),
			UserID: findPayloadValue(attachment.Payload, []string{"user_id", "userId", "id"}),
		})
	}
	return contacts
}

func findPayloadValue(value any, keys []string) string {
	switch item := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if raw, ok := item[key]; ok && fmt.Sprint(raw) != "" {
				return fmt.Sprint(raw)
			}
		}
		for _, raw := range item {
			if found := findPayloadValue(raw, keys); found != "" {
				return found
			}
		}
	case []any:
		for _, raw := range item {
			if found := findPayloadValue(raw, keys); found != "" {
				return found
			}
		}
	}
	return ""
}

func parsePayloadInt(value any, keys []string) int {
	text := findPayloadValue(value, keys)
	var out int
	_, _ = fmt.Sscan(text, &out)
	if strings.Contains(strings.ToLower(strings.Join(keys, ",")), "ms") && out > 1000 {
		return out / 1000
	}
	return out
}

func shouldLogRawMessage(message *maxapi.Message) bool {
	if message == nil {
		return false
	}
	if strings.TrimSpace(message.Body.Text) == "" {
		return true
	}
	for _, attachment := range message.Body.Attachments {
		if attachment.Type == "contact" || attachment.Type == "share" {
			return true
		}
	}
	return false
}

func limitLog(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}
