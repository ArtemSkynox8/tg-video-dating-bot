package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/config"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/db"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/handlers"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/repositories"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/services"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	repo := repositories.New(pool)
	maxClient := maxapi.NewClient(cfg.MaxAPIBaseURL, cfg.MaxBotToken)
	if cfg.MaxBotToken != "" && cfg.PublicBaseURL != "" && cfg.PublicBaseURL != "http://localhost:8080" {
		webhookURL := strings.TrimRight(cfg.PublicBaseURL, "/") + "/webhook/max"
		if err := maxClient.SubscribeWebhook(ctx, webhookURL, cfg.MaxWebhookSecret, []string{"message_created", "message_callback", "bot_started"}); err != nil {
			log.Printf("subscribe max webhook %s: %v", webhookURL, err)
		} else {
			log.Printf("max webhook subscribed: %s", webhookURL)
		}
	}
	botService := services.NewDatingService(repo, maxClient)
	webhook := handlers.NewWebhookHandler(cfg, botService)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("POST /webhook/max", webhook)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("bot listening on %s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
