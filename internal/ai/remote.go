package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

func NewClient(baseURL, secret string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		secret:  strings.TrimSpace(secret),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if c.baseURL == "" { return "", fmt.Errorf("IMAGE_SERVICE_URL is not configured") }
	if c.secret == "" { return "", fmt.Errorf("IMAGE_SERVICE_SECRET is not configured") }
	payload, err := json.Marshal(map[string]any{"messages": messages})
	if err != nil { return "", err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/media/grok-chat", bytes.NewReader(payload))
	if err != nil { return "", err }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Image-Service-Secret", c.secret)
	resp, err := c.http.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil { return "", err }
	var out struct {
		OK    bool   `json:"ok"`
		Reply string `json:"reply"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("grok service returned invalid JSON: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !out.OK {
		if out.Error == "" { out.Error = strings.TrimSpace(string(body)) }
		return "", fmt.Errorf("grok service status %d: %s", resp.StatusCode, out.Error)
	}
	if reply := strings.TrimSpace(out.Reply); reply != "" { return reply, nil }
	return "", fmt.Errorf("grok service returned an empty response")
}
