package maxapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) SendText(ctx context.Context, chatID, text string, buttons [][]Button) error {
	body := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if len(buttons) > 0 {
		body["keyboard"] = buttons
	}
	return c.post(ctx, "/messages", body, nil)
}

func (c *Client) SendMedia(ctx context.Context, chatID, mediaID, caption string, buttons [][]Button) (string, error) {
	body := map[string]any{
		"chat_id":  chatID,
		"media_id": mediaID,
		"caption":  caption,
	}
	if len(buttons) > 0 {
		body["keyboard"] = buttons
	}
	var out struct {
		MessageID string `json:"message_id"`
	}
	if err := c.post(ctx, "/messages/media", body, &out); err != nil {
		return "", err
	}
	return out.MessageID, nil
}

func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID string) error {
	body := map[string]any{"chat_id": chatID, "message_id": messageID}
	return c.post(ctx, "/messages/delete", body, nil)
}

func (c *Client) AnswerCallback(ctx context.Context, callbackID, text string) error {
	body := map[string]any{"callback_id": callbackID, "text": text}
	return c.post(ctx, "/callbacks/answer", body, nil)
}

func (c *Client) post(ctx context.Context, path string, in any, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("max api %s failed: %s", path, res.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

