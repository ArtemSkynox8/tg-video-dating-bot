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
	User       *PlatformUser  `json:"user,omitempty"`
	Message    *Message       `json:"message,omitempty"`
	Callback   *CallbackEvent `json:"callback,omitempty"`
}

type MessageUpdate struct {
	MessageID string       `json:"message_id"`
	Chat      Chat         `json:"chat"`
	From      PlatformUser `json:"from"`
	Text      string       `json:"text,omitempty"`
	Media     []Media      `json:"media,omitempty"`
}

type CallbackUpdate struct {
	CallbackID string       `json:"callback_id"`
	MessageID  string       `json:"message_id"`
	Chat       Chat         `json:"chat"`
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
	u.ProfileLink = valueToString(raw["profile_link"])
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
	Text    string `json:"text"`
	Payload string `json:"payload,omitempty"`
	URL     string `json:"url,omitempty"`
	OpenApp bool  `json:"open_app,omitempty"`
}

type Message struct {
	Sender    PlatformUser `json:"sender"`
	Recipient Recipient   `json:"recipient"`
	Body      MessageBody `json:"body"`
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
	Text        string       `json:"text,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type Attachment struct {
	Type    string            `json:"type"`
	Payload map[string]any    `json:"payload,omitempty"`
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
