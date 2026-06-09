package maxapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	return c.SendMediaToDialogOrUser(ctx, "", userID, mediaID, caption, buttons)
}

func (c *Client) SendMediaToDialogOrUser(ctx context.Context, dialogID, userID, mediaID, caption string, buttons [][]Button) (string, error) {
	videoMessageID, err := c.SendMugToDialogOrUser(ctx, dialogID, userID, mediaID)
	if err != nil {
		return "", err
	}
	if caption == "" && len(buttons) == 0 {
		return videoMessageID, nil
	}
	time.Sleep(1200 * time.Millisecond)
	if err := c.SendText(ctx, userID, caption, buttons); err != nil {
		return "", err
	}
	return videoMessageID, nil
}

func (c *Client) SendMug(ctx context.Context, userID, mediaID string) (string, error) {
	return c.SendMugToDialogOrUser(ctx, "", userID, mediaID)
}

func (c *Client) SendMugToDialogOrUser(ctx context.Context, dialogID, userID, mediaID string) (string, error) {
	if userID != "" {
		messageID, err := c.sendMug(ctx, "/messages?chat_id="+url.QueryEscape(userID), mediaID)
		if err == nil {
			log.Printf("send mug only event_chat=%s token=%s", userID, mediaID)
			return messageID, nil
		}
		log.Printf("send mug by event chat failed chat=%s token=%s: %v", userID, mediaID, err)
	}
	if dialogID != "" {
		messageID, err := c.sendMug(ctx, "/messages?chat_id="+url.QueryEscape(dialogID), mediaID)
		if err == nil {
			log.Printf("send mug only recipient_chat=%s user=%s token=%s", dialogID, userID, mediaID)
			return messageID, nil
		}
		log.Printf("send mug by recipient chat failed chat=%s user=%s token=%s: %v", dialogID, userID, mediaID, err)
	}
	messageID, err := c.sendMug(ctx, "/messages?user_id="+url.QueryEscape(userID), mediaID)
	if err != nil {
		return "", err
	}
	log.Printf("send mug only user=%s token=%s", userID, mediaID)
	return messageID, nil
}

func (c *Client) sendMug(ctx context.Context, path, mediaID string) (string, error) {
	payload := map[string]any{
		"token":      mediaID,
		"format":     "mug",
		"quickVideo": true,
	}
	body := map[string]any{
		"attachments": []map[string]any{
			{"type": "video", "payload": payload, "format": "mug", "quickVideo": true},
		},
	}
	var out struct {
		Message Message `json:"message"`
	}
	if err := c.post(ctx, path, body, &out); err != nil {
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
	uploaded, err := uploadMultipart(ctx, c.http, upload.URL, path)
	if err != nil {
		return "", err
	}
	if token := stringValue(uploaded["token"]); token != "" {
		log.Printf("max upload video completed keys=%s token_source=upload_response", mapKeys(uploaded))
		return token, nil
	}
	log.Printf("max upload video completed keys=%s token_source=upload_url", mapKeys(uploaded))
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

func uploadMultipart(ctx context.Context, client *http.Client, uploadURL, path string) (map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("data", filepath.Base(path))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("max upload failed: %s: %s", res.Status, strings.TrimSpace(string(detail)))
	}
	detail, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(detail))
	if trimmed == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		log.Printf("max upload response is not json content_type=%q prefix=%q", res.Header.Get("Content-Type"), responsePrefix(trimmed))
		return map[string]any{}, nil
	}
	return out, nil
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func mapKeys(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return strings.Join(keys, ",")
}

func responsePrefix(value string) string {
	if len(value) > 120 {
		return value[:120]
	}
	return value
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
