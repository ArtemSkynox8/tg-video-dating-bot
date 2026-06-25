package kinguin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/models"
)

type Client struct {
	cfg  config.Config
	http *http.Client
}

type OrderResult struct {
	OrderID string
	Code    string
}

func NewClient(cfg config.Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Product(ctx context.Context, productID string) (models.ProductQuote, error) {
	path := strings.TrimRight(c.cfg.KinguinProductsPath, "/") + "/" + url.PathEscape(productID)
	var raw map[string]any
	if err := c.request(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return models.ProductQuote{}, err
	}
	offer := firstMapFromArray(raw["offers"])
	return models.ProductQuote{
		ProductID: firstString(raw["id"], raw["productId"], productID),
		Name:     firstString(raw["name"], raw["title"]),
		Price:    firstFloat(raw["price"], raw["priceEur"], raw["cheapestOffer"], raw["originalPrice"], offer["price"]),
		Currency: firstString(raw["currency"], raw["priceCurrency"], offer["currency"], "USD"),
		Qty:      int(firstFloat(raw["qty"], raw["quantity"], raw["stock"], raw["availableQty"], offer["qty"], offer["quantity"])),
	}, nil
}

func (c *Client) CreateOrder(ctx context.Context, productID string, clientOrderID string) (OrderResult, error) {
	body := map[string]any{
		"products": []map[string]any{{
			"productId": productID,
			"qty":       1,
		}},
		"clientOrderId": clientOrderID,
	}
	var raw map[string]any
	if err := c.request(ctx, http.MethodPost, c.cfg.KinguinOrdersPath, body, &raw); err != nil {
		return OrderResult{}, err
	}
	return OrderResult{
		OrderID: firstString(raw["orderId"], raw["id"], raw["_id"]),
		Code:    findCode(raw),
	}, nil
}

func (c *Client) request(ctx context.Context, method, path string, in any, out any) error {
	if c.cfg.KinguinAPIKey == "" {
		return fmt.Errorf("KINGUIN_API_KEY is empty")
	}
	var body io.Reader = http.NoBody
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.KinguinBaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(c.cfg.KinguinAuthHeader, c.cfg.KinguinAPIKey)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	detail, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("kinguin %s %s failed: %s: %s", method, path, res.Status, strings.TrimSpace(string(detail)))
	}
	if out == nil || len(detail) == 0 {
		return nil
	}
	return json.Unmarshal(detail, out)
}

func findCode(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"code", "key", "serial"} {
			if text := firstString(v[key]); text != "" {
				return text
			}
		}
		for _, child := range v {
			if found := findCode(child); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range v {
			if found := findCode(child); found != "" {
				return found
			}
		}
	}
	return ""
}

func firstMapFromArray(value any) map[string]any {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return map[string]any{}
	}
	item, _ := items[0].(map[string]any)
	if item == nil {
		return map[string]any{}
	}
	return item
}

func firstString(values ...any) string {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case float64:
			return fmt.Sprintf("%.0f", v)
		case int:
			return fmt.Sprint(v)
		}
	}
	return ""
}

func firstFloat(values ...any) float64 {
	for _, value := range values {
		switch v := value.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case string:
			var out float64
			if _, err := fmt.Sscan(v, &out); err == nil {
				return out
			}
		}
	}
	return 0
}
