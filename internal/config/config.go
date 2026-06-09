package config

import (
	"os"
	"strings"
)

type Config struct {
	AppEnv           string
	HTTPAddr         string
	PublicBaseURL    string
	MaxAPIBaseURL    string
	MaxBotToken      string
	MaxWebhookSecret string
	DatabaseURL      string
	RedisAddr        string
	UploadDir        string
	AdminPlatformIDs []string
}

func Load() Config {
	httpAddr := getEnv("HTTP_ADDR", "")
	if httpAddr == "" {
		port := getEnv("PORT", "8080")
		httpAddr = ":" + port
	}
	return Config{
		AppEnv:           getEnv("APP_ENV", "development"),
		HTTPAddr:         httpAddr,
		PublicBaseURL:    getEnv("PUBLIC_BASE_URL", "http://localhost:8080"),
		MaxAPIBaseURL:    getEnv("MAX_API_BASE_URL", "https://platform-api.max.ru"),
		MaxBotToken:      os.Getenv("MAX_BOT_TOKEN"),
		MaxWebhookSecret: os.Getenv("MAX_WEBHOOK_SECRET"),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://dating:dating@localhost:5432/dating?sslmode=disable"),
		RedisAddr:        getEnv("REDIS_ADDR", "localhost:6379"),
		UploadDir:        getEnv("UPLOAD_DIR", "/app/uploads"),
		AdminPlatformIDs: splitCSV(os.Getenv("ADMIN_PLATFORM_IDS")),
	}
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
	if strings.TrimSpace(value) == "" {
		return []string{defaultAdmin}
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
