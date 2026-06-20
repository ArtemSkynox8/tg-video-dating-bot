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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil { return "", err }
	var out any
	if err := json.Unmarshal(body, &out); err != nil { return "", err }
	if result := extractAIText(out); result != "" { return result, nil }
	return "", fmt.Errorf("kie api returned no text; response keys=%s", responseKeys(out))
}

func extractAIText(value any) string {
	switch item := value.(type) {
	case string:
		text := strings.TrimSpace(item)
		if text == "" { return "" }
		if json.Valid([]byte(text)) {
			var nested any
			if json.Unmarshal([]byte(text), &nested) == nil {
				if result := extractAIText(nested); result != "" { return result }
			}
		}
		return text
	case []any:
		parts := make([]string, 0, len(item))
		for _, child := range item {
			if text := extractAIText(child); text != "" { parts = append(parts, text) }
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case map[string]any:
		for _, key := range []string{"text", "output_text"} {
			if text, ok := item[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
		for _, key := range []string{"choices", "candidates", "message", "content", "parts", "output", "outputs", "response", "data"} {
			if child, ok := item[key]; ok {
				if text := extractAIText(child); text != "" { return text }
			}
		}
	}
	return ""
}

func responseKeys(value any) string {
	item, ok := value.(map[string]any)
	if !ok { return fmt.Sprintf("%T", value) }
	keys := make([]string, 0, len(item))
	for key := range item { keys = append(keys, key) }
	return strings.Join(keys, ",")
}
