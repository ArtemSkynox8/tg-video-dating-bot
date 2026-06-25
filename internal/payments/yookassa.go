package payments

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
)

type YooKassa struct {
	cfg  config.Config
	http *http.Client
}

type Payment struct {
	ID     string
	Status string
	Paid   bool
	URL    string
}

func NewYooKassa(cfg config.Config) *YooKassa {
	return &YooKassa{cfg: cfg, http: &http.Client{Timeout: 25 * time.Second}}
}

func (y *YooKassa) Enabled() bool {
	return y.cfg.YooKassaShopID != "" && y.cfg.YooKassaSecretKey != ""
}

func (y *YooKassa) Create(ctx context.Context, orderID int64, amount float64, description string) (Payment, error) {
	if !y.Enabled() {
		return Payment{}, fmt.Errorf("YooKassa is not configured")
	}
	returnURL := y.cfg.PublicBaseURL + "/pay/success?order=" + url.QueryEscape(fmt.Sprint(orderID))
	body := map[string]any{
		"amount": map[string]string{
			"value":    fmt.Sprintf("%.2f", amount),
			"currency": "RUB",
		},
		"capture": true,
		"confirmation": map[string]string{
			"type":       "redirect",
			"return_url": returnURL,
		},
		"description": description,
		"metadata": map[string]string{
			"order_id": fmt.Sprint(orderID),
		},
	}
	if y.cfg.YooKassaReceiptEmail != "" {
		body["receipt"] = map[string]any{
			"customer": map[string]string{"email": y.cfg.YooKassaReceiptEmail},
			"items": []map[string]any{{
				"description":     description,
				"quantity":        "1.00",
				"amount":          map[string]string{"value": fmt.Sprintf("%.2f", amount), "currency": "RUB"},
				"vat_code":        1,
				"payment_mode":    "full_payment",
				"payment_subject": "payment",
			}},
		}
	}

	var out yooPaymentResponse
	if err := y.request(ctx, http.MethodPost, "/v3/payments", body, &out, fmt.Sprintf("order-%d-%d", orderID, time.Now().UnixNano())); err != nil {
		return Payment{}, err
	}
	return Payment{ID: out.ID, Status: out.Status, Paid: out.Paid, URL: out.Confirmation.ConfirmationURL}, nil
}

func (y *YooKassa) Get(ctx context.Context, paymentID string) (Payment, error) {
	var out yooPaymentResponse
	if err := y.request(ctx, http.MethodGet, "/v3/payments/"+url.PathEscape(paymentID), nil, &out, ""); err != nil {
		return Payment{}, err
	}
	return Payment{ID: out.ID, Status: out.Status, Paid: out.Paid, URL: out.Confirmation.ConfirmationURL}, nil
}

func (y *YooKassa) request(ctx context.Context, method, path string, in any, out any, idempotenceKey string) error {
	var body io.Reader = http.NoBody
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.yookassa.ru"+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(y.cfg.YooKassaShopID, y.cfg.YooKassaSecretKey)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Idempotence-Key", idempotenceKey)
	}
	res, err := y.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	detail, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("yookassa %s: %s", res.Status, strings.TrimSpace(string(detail)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(detail, out)
}

type yooPaymentResponse struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Paid         bool   `json:"paid"`
	Confirmation struct {
		ConfirmationURL string `json:"confirmation_url"`
	} `json:"confirmation"`
}
