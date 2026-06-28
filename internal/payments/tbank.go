package payments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
)

type TBank struct {
	cfg  config.Config
	http *http.Client
}

type Payment struct {
	ID     string
	Status string
	Paid   bool
	URL    string
}

func NewTBank(cfg config.Config) *TBank {
	return &TBank{cfg: cfg, http: &http.Client{Timeout: 25 * time.Second}}
}

func (t *TBank) Enabled() bool {
	return t.cfg.TBankTerminalKey != "" && t.cfg.TBankPassword != ""
}

func (t *TBank) Create(ctx context.Context, orderID int64, amount float64, description string) (Payment, error) {
	if !t.Enabled() {
		return Payment{}, fmt.Errorf("T-Bank acquiring is not configured")
	}
	returnURL := t.cfg.PublicBaseURL + "/pay/success?order=" + url.QueryEscape(fmt.Sprint(orderID))
	amountKopecks := int64(math.Round(amount * 100))
	body := map[string]any{
		"TerminalKey":     t.cfg.TBankTerminalKey,
		"Amount":          amountKopecks,
		"OrderId":         fmt.Sprint(orderID),
		"Description":     description,
		"SuccessURL":      returnURL,
		"FailURL":         returnURL,
		"NotificationURL": t.cfg.PublicBaseURL + "/pay/tbank/webhook",
		"Receipt": map[string]any{
			"Email":    t.cfg.TBankReceiptEmail,
			"Taxation": t.cfg.TBankTaxation,
			"Items": []map[string]any{
				{
					"Name":          description,
					"Price":         amountKopecks,
					"Quantity":      1,
					"Amount":        amountKopecks,
					"Tax":           t.cfg.TBankReceiptTax,
					"PaymentMethod": "full_payment",
					"PaymentObject": "commodity",
				},
			},
		},
		"DATA": map[string]string{
			"order_id": fmt.Sprint(orderID),
		},
	}
	body["Token"] = t.sign(body)

	var out tbankInitResponse
	if err := t.request(ctx, "/v2/Init", body, &out); err != nil {
		return Payment{}, err
	}
	if !out.Success {
		return Payment{}, fmt.Errorf("tbank init failed: %s %s", out.ErrorCode, firstNonEmpty(out.Message, out.Details))
	}
	paymentID := valueToString(out.PaymentID)
	if paymentID == "" || out.PaymentURL == "" {
		return Payment{}, fmt.Errorf("tbank init response missing payment id or payment url")
	}
	return Payment{ID: paymentID, Status: out.Status, Paid: isPaidStatus(out.Status), URL: out.PaymentURL}, nil
}

func (t *TBank) Get(ctx context.Context, paymentID string) (Payment, error) {
	body := map[string]any{
		"TerminalKey": t.cfg.TBankTerminalKey,
		"PaymentId":   paymentID,
	}
	body["Token"] = t.sign(body)

	var out tbankStateResponse
	if err := t.request(ctx, "/v2/GetState", body, &out); err != nil {
		return Payment{}, err
	}
	if !out.Success {
		return Payment{}, fmt.Errorf("tbank get state failed: %s %s", out.ErrorCode, firstNonEmpty(out.Message, out.Details))
	}
	return Payment{ID: valueToString(out.PaymentID), Status: out.Status, Paid: isPaidStatus(out.Status)}, nil
}

func (t *TBank) request(ctx context.Context, path string, in any, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(t.cfg.TBankBaseURL, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	detail, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("tbank %s: %s", res.Status, strings.TrimSpace(string(detail)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(detail, out)
}

func (t *TBank) sign(values map[string]any) string {
	tokenValues := make(map[string]any, len(values)+1)
	for key, value := range values {
		if key == "Token" {
			continue
		}
		switch value.(type) {
		case map[string]any, map[string]string, []any, []string:
			continue
		}
		tokenValues[key] = value
	}
	tokenValues["Password"] = t.cfg.TBankPassword

	keys := make([]string, 0, len(tokenValues))
	for key := range tokenValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(valueToString(tokenValues[key]))
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func isPaidStatus(status string) bool {
	switch strings.ToUpper(status) {
	case "CONFIRMED", "AUTHORIZED":
		return true
	default:
		return false
	}
}

func valueToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprint(v)
	case int64:
		return fmt.Sprint(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type tbankInitResponse struct {
	Success    bool   `json:"Success"`
	ErrorCode  string `json:"ErrorCode"`
	Message    string `json:"Message"`
	Details    string `json:"Details"`
	Status     string `json:"Status"`
	PaymentID  any    `json:"PaymentId"`
	PaymentURL string `json:"PaymentURL"`
}

type tbankStateResponse struct {
	Success   bool   `json:"Success"`
	ErrorCode string `json:"ErrorCode"`
	Message   string `json:"Message"`
	Details   string `json:"Details"`
	Status    string `json:"Status"`
	PaymentID any    `json:"PaymentId"`
}
