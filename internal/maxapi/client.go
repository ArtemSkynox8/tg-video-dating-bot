package maxapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
		"text": text,
	}
	if len(buttons) > 0 {
		body["attachments"] = inlineKeyboard(buttons)
	}
	return c.post(ctx, "/messages?chat_id="+url.QueryEscape(chatID), body, nil)
}

func (c *Client) SendMedia(ctx context.Context, chatID, mediaID, caption string, buttons [][]Button) (string, error) {
	payload := map[string]any{"token": mediaID}
	if strings.HasPrefix(mediaID, "http://") || strings.HasPrefix(mediaID, "https://") {
		payload = map[string]any{"url": mediaID}
	}
	body := map[string]any{
		"text": caption,
		"attachments": []map[string]any{
			{"type": "video", "payload": payload},
		},
	}
	if len(buttons) > 0 {
		body["attachments"] = append(body["attachments"].([]map[string]any), inlineKeyboard(buttons)...)
	}
	var out struct {
		Message Message `json:"message"`
	}
	if err := c.post(ctx, "/messages?chat_id="+url.QueryEscape(chatID), body, &out); err != nil {
		return "", err
	}
	return out.Message.Body.MID, nil
}

func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID string) error {
	return c.delete(ctx, "/messages?message_id="+url.QueryEscape(messageID))
}

func (c *Client) AnswerCallback(ctx context.Context, callbackID, text string) error {
	body := map[string]any{"notification": text}
	return c.post(ctx, "/answers?callback_id="+url.QueryEscape(callbackID), body, nil)
}

func (c *Client) SubscribeWebhook(ctx context.Context, url, secret string, updateTypes []string) error {
	body := map[string]any{
		"url":          url,
		"update_types": updateTypes,
	}
	if secret != "" {
		body["secret"] = secret
	}
	return c.post(ctx, "/subscriptions", body, nil)
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
		req.Header.Set("Authorization", c.token)
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

func (c *Client) delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("max api %s failed: %s", path, res.Status)
	}
	return nil
}

func inlineKeyboard(buttons [][]Button) []map[string]any {
	rows := make([][]map[string]any, 0, len(buttons))
	for _, row := range buttons {
		outRow := make([]map[string]any, 0, len(row))
		for _, button := range row {
			item := map[string]any{
				"type": "callback",
				"text": button.Text,
			}
			if button.URL != "" && button.OpenApp {
				item["type"] = "open_app"
				item["web_app"] = button.URL
				item["url"] = button.URL
				item["payload"] = button.Payload
			} else if button.URL != "" {
				item["type"] = "link"
				item["url"] = button.URL
			} else {
				item["payload"] = button.Payload
			}
			outRow = append(outRow, item)
		}
		rows = append(rows, outRow)
	}
	return []map[string]any{{
		"type": "inline_keyboard",
		"payload": map[string]any{
			"buttons": rows,
		},
	}}
}
