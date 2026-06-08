package maxapi

type Update struct {
	UpdateID string          `json:"update_id"`
	Message  *MessageUpdate  `json:"message,omitempty"`
	Callback *CallbackUpdate `json:"callback,omitempty"`
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
}

