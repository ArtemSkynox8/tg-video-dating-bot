package config

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Product struct {
	Code             string
	Label            string
	Card             string
	KinguinRetailID  string
	PriceRUB         float64
}

type Config struct {
	HTTPAddr             string
	PublicBaseURL        string
	ReturnToBotURL       string
	SupportURL          string
	MaxAPIBaseURL        string
	MaxBotToken          string
	MaxWebhookSecret     string
	DatabaseURL          string
	AdminPlatformIDs     []string
	KinguinBaseURL       string
	KinguinAPIKey        string
	KinguinAuthHeader    string
	KinguinProductsPath  string
	KinguinPricePath     string
	KinguinOrdersPath    string
	KinguinBalancePath   string
	USDRUBRate           float64
	EURRUBRate           float64
	MarkupPercent        float64
	FixedFeeRUB          float64
	DynamicMarginRUB     float64
	AcquiringFeePercent  float64
	RoundToRUB           float64
	TBankBaseURL         string
	TBankTerminalKey     string
	TBankPassword        string
	TBankReceiptEmail    string
	TBankTaxation        string
	TBankReceiptTax      string
	Products             []Product
}

func Load() Config {
	return Config{
		HTTPAddr:             normalizeHTTPAddr(getEnv("HTTP_ADDR", ""), getEnv("PORT", "8080")),
		PublicBaseURL:        strings.TrimRight(getEnv("PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		ReturnToBotURL:       getEnv("RETURN_TO_BOT_URL", "https://max.ru/id550411830268_4_bot"),
		SupportURL:           getEnv("SUPPORT_URL", "https://max.ru/u/f9LHodD0cOIeNOmeypwAxDR6yT3wvd5VJ7oPdXUk4OlmfT2vcctcWqOcTkk"),
		MaxAPIBaseURL:        getEnv("MAX_API_BASE_URL", "https://platform-api.max.ru"),
		MaxBotToken:          os.Getenv("MAX_BOT_TOKEN"),
		MaxWebhookSecret:     os.Getenv("MAX_WEBHOOK_SECRET"),
		DatabaseURL:          getEnv("DATABASE_URL", "postgres://robux:robux@localhost:5432/robux?sslmode=disable"),
		AdminPlatformIDs:     splitCSV(os.Getenv("ADMIN_PLATFORM_IDS")),
		KinguinBaseURL:       normalizeKinguinBaseURL(getEnv("KINGUIN_BASE_URL", "https://gateway.kinguin.net")),
		KinguinAPIKey:        os.Getenv("KINGUIN_API_KEY"),
		KinguinAuthHeader:    getEnv("KINGUIN_AUTH_HEADER", "X-Api-Key"),
		KinguinProductsPath:  getEnv("KINGUIN_PRODUCTS_PATH", "/esa/api/v2/products"),
		KinguinPricePath:     getEnv("KINGUIN_PRICE_PATH", ""),
		KinguinOrdersPath:    getEnv("KINGUIN_ORDERS_PATH", "/esa/api/v2/order"),
		KinguinBalancePath:   getEnv("KINGUIN_BALANCE_PATH", "/esa/api/v2/account/balance"),
		USDRUBRate:           floatEnv("USD_RUB_RATE", 90),
		EURRUBRate:           floatEnv("EUR_RUB_RATE", 100),
		MarkupPercent:        floatEnv("MARKUP_PERCENT", 0),
		FixedFeeRUB:          floatEnv("FIXED_FEE_RUB", 0),
		DynamicMarginRUB:     floatEnv("DYNAMIC_MARGIN_RUB", 200),
		AcquiringFeePercent:  floatEnv("ACQUIRING_FEE_PERCENT", 5),
		RoundToRUB:           floatEnv("ROUND_TO_RUB", 1),
		TBankBaseURL:         getEnv("TBANK_BASE_URL", "https://securepay.tinkoff.ru"),
		TBankTerminalKey:     os.Getenv("TBANK_TERMINAL_KEY"),
		TBankPassword:        os.Getenv("TBANK_PASSWORD"),
		TBankReceiptEmail:    getEnv("TBANK_RECEIPT_EMAIL", "test@example.com"),
		TBankTaxation:        getEnv("TBANK_TAXATION", "usn_income"),
		TBankReceiptTax:      getEnv("TBANK_RECEIPT_TAX", "none"),
		Products:             loadProducts(),
	}
}

func loadProducts() []Product {
	products := []Product{
		{Code: "400", Label: "400 Robux", Card: "Region Free", KinguinRetailID: firstNonEmptyEnv("ROBLOX_400_KINGUIN_ID", "PRODUCT_400_ROBUX", "107368"), PriceRUB: floatEnv("ROBUX_400_PRICE_RUB", 499)},
		{Code: "800", Label: "800 Robux", Card: "Region Free", KinguinRetailID: firstNonEmptyEnv("ROBLOX_800_KINGUIN_ID", "PRODUCT_800_ROBUX", "107369"), PriceRUB: floatEnv("ROBUX_800_PRICE_RUB", 899)},
		{Code: "2000", Label: "2000 Robux", Card: "Region Free", KinguinRetailID: firstNonEmptyEnv("ROBLOX_2000_KINGUIN_ID", "PRODUCT_2000_ROBUX", "107371"), PriceRUB: floatEnv("ROBUX_2000_PRICE_RUB", 2199)},
	}
	return products
}

func normalizeHTTPAddr(httpAddr, port string) string {
	httpAddr = strings.TrimSpace(httpAddr)
	port = strings.TrimSpace(port)
	if port == "" {
		port = "8080"
	}
	if httpAddr == "" {
		return "0.0.0.0:" + port
	}
	if regexp.MustCompile(`^\d+$`).MatchString(httpAddr) {
		return "0.0.0.0:" + httpAddr
	}
	if strings.HasPrefix(httpAddr, ":") {
		return "0.0.0.0" + httpAddr
	}
	return httpAddr
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func firstNonEmptyEnv(keysAndFallback ...string) string {
	if len(keysAndFallback) == 0 {
		return ""
	}
	fallback := keysAndFallback[len(keysAndFallback)-1]
	for _, key := range keysAndFallback[:len(keysAndFallback)-1] {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func normalizeKinguinBaseURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	switch value {
	case "https://kinguin.net", "https://www.kinguin.net", "http://kinguin.net", "http://www.kinguin.net":
		return "https://gateway.kinguin.net"
	default:
		return value
	}
}

func floatEnv(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64)
	if err != nil || value == 0 {
		return fallback
	}
	return value
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
