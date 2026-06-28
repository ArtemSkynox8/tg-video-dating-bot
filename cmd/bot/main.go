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
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/kinguin"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/maxapi"
	"github.com/ArtemSkynox8/tg-video-dating-bot/internal/payments"
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
	kinguinClient := kinguin.NewClient(cfg)
	tbank := payments.NewTBank(cfg)
	shop := services.NewShopService(cfg, repo, maxClient, kinguinClient, tbank)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", ok)
	mux.HandleFunc("GET /healthz", ok)
	mux.Handle("POST /webhook/max", handlers.NewWebhookHandler(cfg, shop))
	handlers.NewPaymentHandler(repo, tbank, shop).Register(mux)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if cfg.MaxBotToken != "" && cfg.PublicBaseURL != "" && cfg.PublicBaseURL != "http://localhost:8080" {
		webhookURL := strings.TrimRight(cfg.PublicBaseURL, "/") + "/webhook/max"
		if err := maxClient.SubscribeWebhook(ctx, webhookURL, cfg.MaxWebhookSecret, []string{"message_created", "message_callback", "bot_started"}); err != nil {
			log.Printf("subscribe max webhook %s: %v", webhookURL, err)
		} else {
			log.Printf("max webhook subscribed: %s", webhookURL)
		}
		if err := maxClient.SetCommands(ctx, []maxapi.Command{
			{Name: "start", Description: "Главное меню"},
			{Name: "stats", Description: "Статистика заказов"},
		}); err != nil {
			log.Printf("set max commands: %v", err)
		}
	}

	go func() {
		log.Printf("robux bot listening on %s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func ok(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
