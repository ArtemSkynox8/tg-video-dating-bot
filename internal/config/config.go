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
	KinguinProductID string
	PriceRUB         float64
}

type Config struct {
	HTTPAddr             string
	PublicBaseURL        string
	ReturnToBotURL       string
	MaxAPIBaseURL        string
	MaxBotToken          string
	MaxWebhookSecret     string
	DatabaseURL          string
	AdminPlatformIDs     []string
	KinguinBaseURL       string
	KinguinAPIKey        string
	KinguinAuthHeader    string
	KinguinProductsPath  string
	KinguinOrdersPath    string
	USDRUBRate           float64
	EURRUBRate           float64
	MarkupPercent        float64
	FixedFeeRUB          float64
	RoundToRUB           float64
	YooKassaShopID       string
	YooKassaSecretKey    string
	YooKassaReceiptEmail string
	Products             []Product
}

func Load() Config {
	return Config{
		HTTPAddr:             normalizeHTTPAddr(getEnv("HTTP_ADDR", ""), getEnv("PORT", "8080")),
		PublicBaseURL:        strings.TrimRight(getEnv("PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		ReturnToBotURL:       getEnv("RETURN_TO_BOT_URL", "https://max.ru/id550411830268_4_bot"),
		MaxAPIBaseURL:        getEnv("MAX_API_BASE_URL", "https://platform-api.max.ru"),
		MaxBotToken:          os.Getenv("MAX_BOT_TOKEN"),
		MaxWebhookSecret:     os.Getenv("MAX_WEBHOOK_SECRET"),
		DatabaseURL:          getEnv("DATABASE_URL", "postgres://robux:robux@localhost:5432/robux?sslmode=disable"),
		AdminPlatformIDs:     splitCSV(os.Getenv("ADMIN_PLATFORM_IDS")),
		KinguinBaseURL:       getEnv("KINGUIN_BASE_URL", "https://kinguin.net"),
		KinguinAPIKey:        os.Getenv("KINGUIN_API_KEY"),
		KinguinAuthHeader:    getEnv("KINGUIN_AUTH_HEADER", "X-Api-Key"),
		KinguinProductsPath:  getEnv("KINGUIN_PRODUCTS_PATH", "/esa/api/v2/products"),
		KinguinOrdersPath:    getEnv("KINGUIN_ORDERS_PATH", "/esa/api/v2/orders"),
		USDRUBRate:           floatEnv("USD_RUB_RATE", 90),
		EURRUBRate:           floatEnv("EUR_RUB_RATE", 100),
		MarkupPercent:        floatEnv("MARKUP_PERCENT", 20),
		FixedFeeRUB:          floatEnv("FIXED_FEE_RUB", 0),
		RoundToRUB:           floatEnv("ROUND_TO_RUB", 1),
		YooKassaShopID:       os.Getenv("YOOKASSA_SHOP_ID"),
		YooKassaSecretKey:    os.Getenv("YOOKASSA_SECRET_KEY"),
		YooKassaReceiptEmail: getEnv("YOOKASSA_RECEIPT_EMAIL", ""),
		Products:             loadProducts(),
	}
}

func loadProducts() []Product {
	return []Product{
		{Code: "400", Label: "400 Robux", Card: "$5", KinguinProductID: os.Getenv("PRODUCT_400_ROBUX"), PriceRUB: floatEnv("ROBUX_400_PRICE_RUB", 499)},
		{Code: "800", Label: "800 Robux", Card: "$10", KinguinProductID: os.Getenv("PRODUCT_800_ROBUX"), PriceRUB: floatEnv("ROBUX_800_PRICE_RUB", 899)},
		{Code: "2000", Label: "2000 Robux", Card: "$25", KinguinProductID: os.Getenv("PRODUCT_2000_ROBUX"), PriceRUB: floatEnv("ROBUX_2000_PRICE_RUB", 2199)},
		{Code: "4500", Label: "4500 Robux", Card: "$50", KinguinProductID: os.Getenv("PRODUCT_4500_ROBUX"), PriceRUB: floatEnv("ROBUX_4500_PRICE_RUB", 4299)},
	}
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
