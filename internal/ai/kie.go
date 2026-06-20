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
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "https://api.kie.ai/v1" || baseURL == "https://api.kie.ai/api/v1" {
		baseURL = "https://api.kie.ai/gemini-3-5-flash-openai/v1"
	}
	return &Client{
		baseURL: baseURL,
		apiKey: strings.TrimSpace(apiKey),
		model: strings.TrimSpace(model),
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("KIE_API_KEY is not configured")
	}
	wireMessages := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		role := message.Role
		if role == "assistant" {
			role = "model"
		}
		wireMessages = append(wireMessages, map[string]any{
			"role": role,
			"content": []map[string]string{{"type": "text", "text": message.Content}},
		})
	}
	payload, err := json.Marshal(map[string]any{
		"messages": wireMessages,
		"stream": false,
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
	var out struct {
		Choices []struct { Message Message `json:"message"` } `json:"choices"`
		Candidates []struct {
			Content struct {
				Parts []struct { Text string `json:"text"` } `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return "", err }
	if len(out.Choices) > 0 && strings.TrimSpace(out.Choices[0].Message.Content) != "" {
		return strings.TrimSpace(out.Choices[0].Message.Content), nil
	}
	if len(out.Candidates) > 0 {
		parts := make([]string, 0, len(out.Candidates[0].Content.Parts))
		for _, part := range out.Candidates[0].Content.Parts {
			if text := strings.TrimSpace(part.Text); text != "" { parts = append(parts, text) }
		}
		if result := strings.TrimSpace(strings.Join(parts, "\n")); result != "" { return result, nil }
	}
	return "", fmt.Errorf("kie api returned an empty response")
}
