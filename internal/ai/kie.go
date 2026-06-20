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
	apiKey  string
	model   string
	http    *http.Client
}

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey: strings.TrimSpace(apiKey),
		model: strings.TrimSpace(model),
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("KIE_API_KEY is not configured")
	}
	payload, err := json.Marshal(map[string]any{
		"model": c.model, "messages": messages, "temperature": 0.9, "max_tokens": 500,
	})
	if err != nil { return "", err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil { return "", err }
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("kie api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct { Choices []struct { Message Message `json:"message"` } `json:"choices"` }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return "", err }
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("kie api returned an empty response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
