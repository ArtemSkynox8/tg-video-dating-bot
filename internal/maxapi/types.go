package maxapi

import (
	"encoding/json"
	"fmt"
)

type Update struct {
	UpdateID   string         `json:"update_id"`
	UpdateType string         `json:"update_type"`
	Timestamp  int64          `json:"timestamp"`
	ChatID     int64          `json:"chat_id,omitempty"`
	Payload    string         `json:"payload,omitempty"`
	StartParam string         `json:"start_param,omitempty"`
	StartParamCamel string    `json:"startParam,omitempty"`
	StartPayload string       `json:"start_payload,omitempty"`
	User       *PlatformUser  `json:"user,omitempty"`
	Message    *Message       `json:"message,omitempty"`
	Callback   *CallbackEvent `json:"callback,omitempty"`
}

type MessageUpdate struct {
	MessageID string       `json:"message_id"`
	Chat      Chat         `json:"chat"`
	Dialog    Chat         `json:"dialog,omitempty"`
	From      PlatformUser `json:"from"`
	Text      string       `json:"text,omitempty"`
	Media     []Media      `json:"media,omitempty"`
	Contacts  []Contact    `json:"contacts,omitempty"`
	Forward   *ForwardInfo `json:"forward,omitempty"`
	ImageURLs []string     `json:"image_urls,omitempty"`
}

type CallbackUpdate struct {
	CallbackID string       `json:"callback_id"`
	MessageID  string       `json:"message_id"`
	Chat       Chat         `json:"chat"`
	Dialog     Chat         `json:"dialog,omitempty"`
	From       PlatformUser `json:"from"`
	Payload    string       `json:"payload"`
}

type PlatformUser struct {
	ID          string `json:"user_id"`
	Username    string `json:"username,omitempty"`
	ProfileLink string `json:"profile_link,omitempty"`
	Name        string `json:"name,omitempty"`
	FirstName   string `json:"first_name,omitempty"`
	LastName    string `json:"last_name,omitempty"`
}

func (u *PlatformUser) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	u.ID = valueToString(raw["user_id"])
	if u.ID == "" {
		u.ID = valueToString(raw["id"])
	}
	u.Username = valueToString(raw["username"])
	u.ProfileLink = firstString(raw, []string{"profile_link", "profileLink", "link", "url", "share_link", "shareLink", "public_link", "publicLink"})
	u.Name = valueToString(raw["name"])
	u.FirstName = valueToString(raw["first_name"])
	u.LastName = valueToString(raw["last_name"])
	return nil
}

type Chat struct {
	ID string `json:"chat_id"`
}

type Media struct {
	ID       string `json:"media_id"`
	Type     string `json:"type"`
	Duration int    `json:"duration,omitempty"`
	URL      string `json:"url,omitempty"`
}

type Button struct {
	Text           string `json:"text"`
	Payload        string `json:"payload,omitempty"`
	URL            string `json:"url,omitempty"`
	OpenApp        bool   `json:"open_app,omitempty"`
	RequestContact bool   `json:"request_contact,omitempty"`
}

type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Message struct {
	Sender    PlatformUser `json:"sender"`
	Recipient Recipient   `json:"recipient"`
	Body      MessageBody `json:"body"`
	Link      *MessageLink `json:"link,omitempty"`
}

type Recipient struct {
	ChatID string `json:"chat_id,omitempty"`
	UserID string `json:"user_id,omitempty"`
}

func (r *Recipient) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.ChatID = valueToString(raw["chat_id"])
	r.UserID = valueToString(raw["user_id"])
	return nil
}

type MessageBody struct {
	MID         string       `json:"mid,omitempty"`
	Seq         json.Number  `json:"seq,omitempty"`
	Text        string       `json:"text,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type Attachment struct {
	Type    string            `json:"type"`
	Payload map[string]any    `json:"payload,omitempty"`
}

type Contact struct {
	Name   string
	Phone  string
	UserID string
}

type MessageLink struct {
	Type    string       `json:"type,omitempty"`
	Message *MessageBody `json:"message,omitempty"`
	Sender  PlatformUser `json:"sender,omitempty"`
	ChatID  any          `json:"chat_id,omitempty"`
}

type ForwardInfo struct {
	MID        string
	Seq        string
	Text       string
	SenderID   string
	SenderName string
	ChatID     string
}

type CallbackEvent struct {
	CallbackID string  `json:"callback_id"`
	Payload    string  `json:"payload"`
	User       PlatformUser `json:"user"`
	Message    Message `json:"message"`
}

func valueToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	default:
		return fmt.Sprint(v)
	}
}

func firstString(values map[string]any, keys []string) string {
	for _, key := range keys {
		if value := valueToString(values[key]); value != "" {
			return value
		}
	}
	for _, key := range keys {
		nested, ok := values[key].(map[string]any)
		if !ok {
			continue
		}
		if value := firstString(nested, keys); value != "" {
			return value
		}
	}
	return ""
}
