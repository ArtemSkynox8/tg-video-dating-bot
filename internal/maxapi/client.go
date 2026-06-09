package maxapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

func (c *Client) SendText(ctx context.Context, userID, text string, buttons [][]Button) error {
	body := map[string]any{
		"text": text,
	}
	if len(buttons) > 0 {
		body["attachments"] = inlineKeyboard(buttons)
	}
	return c.post(ctx, "/messages?user_id="+url.QueryEscape(userID), body, nil)
}

func (c *Client) SendMedia(ctx context.Context, userID, mediaID, caption string, buttons [][]Button) (string, error) {
	payload := map[string]any{
		"token":      mediaID,
		"format":     "mug",
		"quickVideo": true,
	}
	body := map[string]any{
		"text": caption,
		"attachments": []map[string]any{
			{"type": "video", "payload": payload, "quickVideo": true},
		},
	}
	if len(buttons) > 0 {
		body["attachments"] = append(body["attachments"].([]map[string]any), inlineKeyboard(buttons)...)
	}
	var out struct {
		Message Message `json:"message"`
	}
	if err := c.post(ctx, "/messages?user_id="+url.QueryEscape(userID), body, &out); err != nil {
		return "", err
	}
	return out.Message.Body.MID, nil
}

func (c *Client) UploadVideo(ctx context.Context, path string) (string, error) {
	var upload struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := c.post(ctx, "/uploads?type=video", nil, &upload); err != nil {
		return "", err
	}
	if upload.URL == "" || upload.Token == "" {
		return "", fmt.Errorf("max upload url response missing url or token")
	}
	if err := uploadMultipart(ctx, c.http, upload.URL, path); err != nil {
		return "", err
	}
	return upload.Token, nil
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
	var reader io.Reader
	if in == nil {
		reader = http.NoBody
	} else {
		payload, err := json.Marshal(in)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
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
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("max api %s failed: %s: %s", path, res.Status, strings.TrimSpace(string(detail)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func uploadMultipart(ctx context.Context, client *http.Client, uploadURL, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("data", filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("max upload failed: %s: %s", res.Status, strings.TrimSpace(string(detail)))
	}
	return nil
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
