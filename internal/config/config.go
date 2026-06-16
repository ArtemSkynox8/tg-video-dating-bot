package config

import (
	"os"
	"regexp"
	"strings"
)

type Config struct {
	AppEnv                    string
	HTTPAddr                  string
	PublicBaseURL             string
	ReturnToBotURL            string
	MaxAPIBaseURL             string
	MaxBotToken               string
	MaxWebhookSecret          string
	DatabaseURL               string
	RedisAddr                 string
	UploadDir                 string
	AdminPlatformIDs          []string
	YooKassaShopID            string
	YooKassaSecretKey         string
	YooKassaReceiptEmail      string
	PremiumPrice              string
	ContactInstructionVideoID string
	ContactInstructionVideoPath string
	FortuneWheelVideoID       string
	FortuneWheelVideoPath     string
}

func Load() Config {
	httpAddr := normalizeHTTPAddr(getEnv("HTTP_ADDR", ""), getEnv("PORT", "8080"))
	return Config{
		AppEnv:                    getEnv("APP_ENV", "development"),
		HTTPAddr:                  httpAddr,
		PublicBaseURL:             getEnv("PUBLIC_BASE_URL", "http://localhost:8080"),
		ReturnToBotURL:            getEnv("RETURN_TO_BOT_URL", "https://max.ru/id550411830268_1_bot"),
		MaxAPIBaseURL:             getEnv("MAX_API_BASE_URL", "https://platform-api.max.ru"),
		MaxBotToken:               os.Getenv("MAX_BOT_TOKEN"),
		MaxWebhookSecret:          os.Getenv("MAX_WEBHOOK_SECRET"),
		DatabaseURL:               getEnv("DATABASE_URL", "postgres://dating:dating@localhost:5432/dating?sslmode=disable"),
		RedisAddr:                 getEnv("REDIS_ADDR", "localhost:6379"),
		UploadDir:                 getEnv("UPLOAD_DIR", "/app/uploads"),
		AdminPlatformIDs:          splitCSV(os.Getenv("ADMIN_PLATFORM_IDS")),
		YooKassaShopID:            os.Getenv("YOOKASSA_SHOP_ID"),
		YooKassaSecretKey:         os.Getenv("YOOKASSA_SECRET_KEY"),
		YooKassaReceiptEmail:      getEnv("YOOKASSA_RECEIPT_EMAIL", "artem.skynox@yandex.ru"),
		PremiumPrice:              getEnv("PREMIUM_PRICE", "199.00"),
		ContactInstructionVideoID: os.Getenv("CONTACT_INSTRUCTION_VIDEO_ID"),
		ContactInstructionVideoPath: getEnv("CONTACT_INSTRUCTION_VIDEO_PATH", "assets/contact-instruction/instruction.mp4"),
		FortuneWheelVideoID:       os.Getenv("FORTUNE_WHEEL_VIDEO_ID"),
		FortuneWheelVideoPath:     getEnv("FORTUNE_WHEEL_VIDEO_PATH", "assets/fortune-wheel/wheel.mp4"),
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
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func splitCSV(value string) []string {
	defaultAdmin := "5156654"
	extraAdmin := "4533898"
	if strings.TrimSpace(value) == "" {
		return []string{defaultAdmin, extraAdmin}
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if !contains(out, defaultAdmin) {
		out = append(out, defaultAdmin)
	}
	if !contains(out, extraAdmin) {
		out = append(out, extraAdmin)
	}
	return out
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
