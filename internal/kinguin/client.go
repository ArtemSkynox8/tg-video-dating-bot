package kinguin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	Details string
}

func NewClient(cfg config.Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Product(ctx context.Context, productID string) (models.ProductQuote, error) {
	return c.productAtPath(ctx, strings.TrimRight(c.cfg.KinguinProductsPath, "/")+"/"+url.PathEscape(productID), productID)
}

func (c *Client) ResolveRetailProduct(ctx context.Context, retailID string) (models.ProductQuote, error) {
	basePath := strings.TrimRight(c.cfg.KinguinProductsPath, "/")
	paths := []string{
		basePath + "?kinguinId=" + url.QueryEscape(retailID),
		basePath + "?kinguinID=" + url.QueryEscape(retailID),
		basePath + "?kinguin_id=" + url.QueryEscape(retailID),
		basePath + "?kinguinId[]=" + url.QueryEscape(retailID),
		basePath + "?kinguinId[0]=" + url.QueryEscape(retailID),
		basePath + "?externalId=" + url.QueryEscape(retailID),
		basePath + "?external_id=" + url.QueryEscape(retailID),
		basePath + "?id=" + url.QueryEscape(retailID),
		basePath + "?q=" + url.QueryEscape(retailID),
		basePath + "?search=" + url.QueryEscape(retailID),
		basePath + "?phrase=" + url.QueryEscape(retailID),
	}
	errors := []string{}
	for _, path := range uniqueStrings(paths) {
		quote, err := c.firstProductFromCatalog(ctx, path, retailID)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		return quote, nil
	}
	return models.ProductQuote{}, fmt.Errorf("kinguin retail product lookup failed: %s", strings.Join(errors, " | "))
}

func (c *Client) PriceAndStock(ctx context.Context, productID string) (models.ProductQuote, error) {
	productPaths := []string{
		strings.TrimRight(c.cfg.KinguinProductsPath, "/") + "/" + url.PathEscape(productID),
		"/esa/api/v2/products/" + url.PathEscape(productID),
	}
	errors := []string{}
	for _, path := range uniqueStrings(productPaths) {
		quote, err := c.productAtPath(ctx, path, productID)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		return quote, nil
	}

	pricePaths := []string{
		productPath(c.cfg.KinguinPricePath, productID),
		productPath("/esa/api/v2/products/{id}/price", productID),
	}
	for _, path := range uniqueStrings(pricePaths) {
		if path == "" {
			continue
		}
		var raw map[string]any
		if err := c.request(ctx, http.MethodGet, path, nil, &raw); err != nil {
			errors = append(errors, err.Error())
			continue
		}
		return quoteFromRaw(productID, raw), nil
	}
	return models.ProductQuote{}, fmt.Errorf("kinguin price and product checks failed: %s", strings.Join(errors, " | "))
}

func (c *Client) firstProductFromCatalog(ctx context.Context, path, retailID string) (models.ProductQuote, error) {
	var raw any
	if err := c.request(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return models.ProductQuote{}, err
	}
	products := productMaps(raw)
	for _, product := range products {
		if retailID != "" && !productHasRetailID(product, retailID) {
			continue
		}
		quote := quoteFromRaw("", product)
		if quote.ProductID != "" {
			return quote, nil
		}
	}
	return models.ProductQuote{}, fmt.Errorf("kinguin catalog lookup returned no product for retail id %s; returned=%d; sample=%s", retailID, len(products), productSample(products))
}

func (c *Client) productAtPath(ctx context.Context, path, productID string) (models.ProductQuote, error) {
	var raw map[string]any
	if err := c.request(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return models.ProductQuote{}, err
	}
	return quoteFromRaw(productID, raw), nil
}

func productMaps(value any) []map[string]any {
	switch v := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"data", "results", "items", "products", "_embedded"} {
			if items := productMaps(v[key]); len(items) > 0 {
				return items
			}
		}
		for _, value := range v {
			if items := productMaps(value); len(items) > 0 {
				return items
			}
		}
		return []map[string]any{v}
	default:
		return nil
	}
}

func quoteFromRaw(productID string, raw map[string]any) models.ProductQuote {
	offer := firstMapFromArray(firstNonNil(raw["offers"], raw["data"], raw["items"]))
	priceMap := firstMapFromArray(raw["price"])
	price := firstFloat(raw["price"], raw["priceEur"], raw["priceUsd"], raw["amount"], raw["minPrice"], raw["lowestPrice"], raw["cheapestOffer"], priceMap["amount"], priceMap["value"], priceMap["price"], offer["price"], offer["amount"])
	currency := firstString(raw["currency"], raw["priceCurrency"], raw["curr"], priceMap["currency"], offer["currency"], "EUR")
	qty := int(firstFloat(raw["qty"], raw["quantity"], raw["stock"], raw["availableQty"], raw["available"], raw["inStock"], offer["qty"], offer["quantity"], offer["stock"]))
	return models.ProductQuote{
		ProductID: firstString(raw["id"], raw["productId"], productID),
		Name:      firstString(raw["name"], raw["title"]),
		Price:     price,
		Currency:  currency,
		Qty:       qty,
	}
}

func productRetailID(product map[string]any) string {
	for key, raw := range product {
		if !isRetailIDKey(key) {
			continue
		}
		if value := firstString(raw); value != "" {
			return value
		}
	}
	for _, value := range product {
		switch nested := value.(type) {
		case map[string]any:
			if found := productRetailID(nested); found != "" {
				return found
			}
		case []any:
			for _, item := range nested {
				mapped, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if found := productRetailID(mapped); found != "" {
					return found
				}
			}
		}
	}
	return ""
}

func productHasRetailID(value any, retailID string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, raw := range typed {
			if isRetailIDKey(key) && firstString(raw) == retailID {
				return true
			}
			if productHasRetailID(raw, retailID) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if productHasRetailID(item, retailID) {
				return true
			}
		}
	}
	return false
}

func isRetailIDKey(key string) bool {
	switch key {
	case "kinguinId", "kinguinID", "kinguin_id", "retailId", "retailID", "retail_id", "externalId", "externalID", "external_id":
		return true
	default:
		return false
	}
}

func productSample(products []map[string]any) string {
	if len(products) == 0 {
		return "empty"
	}
	limit := len(products)
	if limit > 3 {
		limit = 3
	}
	parts := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		product := products[i]
		parts = append(parts, fmt.Sprintf("{id=%s productId=%s kinguinId=%s name=%q keys=%s}",
			firstString(product["id"]),
			firstString(product["productId"]),
			productRetailID(product),
			firstString(product["name"], product["title"]),
			strings.Join(sortedKeys(product), ",")))
	}
	return strings.Join(parts, "; ")
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 12 {
		return keys[:12]
	}
	return keys
}

func (c *Client) CreateOrder(ctx context.Context, productID string, price float64, clientOrderID string) (OrderResult, error) {
	body := map[string]any{
		"products": []map[string]any{{
			"productId": productID,
			"price":     price,
			"qty":       1,
		}},
		"clientOrderId": clientOrderID,
	}
	paths := []string{
		c.cfg.KinguinOrdersPath,
		"/esa/api/v2/order",
		"/esa/api/v2/orders",
		"/api/v2/order",
		"/api/v2/orders",
	}
	errors := []string{}
	for _, path := range uniqueStrings(paths) {
		var raw map[string]any
		if err := c.request(ctx, http.MethodPost, path, body, &raw); err != nil {
			errors = append(errors, err.Error())
			continue
		}
		result := OrderResult{
			OrderID: firstString(raw["orderId"], raw["id"], raw["_id"]),
			Code:    findCode(raw),
			Details: responseSample(raw),
		}
		if result.Code == "" && result.OrderID != "" {
			code, details := c.GetOrderCode(ctx, result.OrderID)
			result.Code = code
			if details != "" {
				result.Details = details
			}
		}
		return result, nil
	}
	return OrderResult{}, fmt.Errorf("kinguin create order failed: %s", strings.Join(errors, " | "))
}

func (c *Client) GetOrderCode(ctx context.Context, orderID string) (string, string) {
	paths := []string{
		"/esa/api/v2/order/" + url.PathEscape(orderID),
		"/esa/api/v2/orders/" + url.PathEscape(orderID),
		"/esa/api/v2/order/" + url.PathEscape(orderID) + "/keys",
		"/esa/api/v2/orders/" + url.PathEscape(orderID) + "/keys",
		"/api/v2/order/" + url.PathEscape(orderID),
		"/api/v2/orders/" + url.PathEscape(orderID),
	}
	errors := []string{}
	for _, path := range uniqueStrings(paths) {
		var raw any
		if err := c.request(ctx, http.MethodGet, path, nil, &raw); err != nil {
			errors = append(errors, err.Error())
			continue
		}
		if code := findCode(raw); code != "" {
			return code, responseSample(raw)
		}
		errors = append(errors, "no code at "+path+": "+responseSample(raw))
	}
	return "", strings.Join(errors, " | ")
}

func (c *Client) Balance(ctx context.Context, currency string) (float64, error) {
	paths := []string{
		c.cfg.KinguinBalancePath,
		"/esa/api/v2/account/balance",
		"/esa/api/v2/balance",
		"/api/v2/account/balance",
		"/api/v2/balance",
	}
	errors := []string{}
	for _, path := range uniqueStrings(paths) {
		var raw any
		if err := c.request(ctx, http.MethodGet, path, nil, &raw); err != nil {
			errors = append(errors, err.Error())
			continue
		}
		if amount, ok := findBalance(raw, currency); ok {
			return amount, nil
		}
		errors = append(errors, "balance not found at "+path+": "+responseSample(raw))
	}
	return 0, fmt.Errorf("kinguin balance check failed: %s", strings.Join(errors, " | "))
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

func productPath(template, productID string) string {
	if strings.TrimSpace(template) == "" {
		return ""
	}
	escapedID := url.PathEscape(productID)
	switch {
	case strings.Contains(template, "{id}"):
		return strings.ReplaceAll(template, "{id}", escapedID)
	case strings.Contains(template, "%s"):
		return fmt.Sprintf(template, escapedID)
	default:
		return strings.TrimRight(template, "/") + "/" + escapedID + "/price"
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func findCode(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"code", "key", "serial", "giftCode", "gift_code", "activationCode", "activation_code", "redeemCode", "redeem_code", "licenseKey", "license_key"} {
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

func responseSample(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%T", value)
	}
	text := string(payload)
	if len(text) > 1200 {
		return text[:1200]
	}
	return text
}

func findBalance(value any, currency string) (float64, bool) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	switch v := value.(type) {
	case map[string]any:
		if amount, ok := firstFloatOK(v[currency], v[strings.ToLower(currency)]); ok {
			return amount, true
		}
		itemCurrency := strings.ToUpper(firstString(v["currency"], v["curr"], v["code"], v["name"]))
		if itemCurrency == currency {
			if amount, ok := firstFloatOK(v["balance"], v["amount"], v["available"], v["availableBalance"], v["value"]); ok {
				return amount, true
			}
		}
		for _, key := range []string{"balances", "balance", "data", "items", "wallets", "accounts"} {
			if amount, ok := findBalance(v[key], currency); ok {
				return amount, true
			}
		}
		for _, child := range v {
			if amount, ok := findBalance(child, currency); ok {
				return amount, true
			}
		}
	case []any:
		for _, child := range v {
			if amount, ok := findBalance(child, currency); ok {
				return amount, true
			}
		}
	}
	return 0, false
}

func firstFloatOK(values ...any) (float64, bool) {
	for _, value := range values {
		switch v := value.(type) {
		case float64:
			return v, true
		case int:
			return float64(v), true
		case bool:
			if v {
				return 1, true
			}
			return 0, true
		case string:
			var out float64
			if _, err := fmt.Sscan(v, &out); err == nil {
				return out, true
			}
		}
	}
	return 0, false
}

func firstMapFromArray(value any) map[string]any {
	if item, ok := value.(map[string]any); ok {
		return item
	}
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

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
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
		case bool:
			if v {
				return 1
			}
			return 0
		case string:
			var out float64
			if _, err := fmt.Sscan(v, &out); err == nil {
				return out
			}
		}
	}
	return 0
}
