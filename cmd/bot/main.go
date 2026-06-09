package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
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

	webhook := newDynamicWebhook()
	miniapp := newDynamicWebhook()

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
	mux.Handle("GET /mini/", miniapp)
	mux.Handle("POST /mini/", miniapp)
	mux.Handle("GET /media/", miniapp)
	mux.Handle("GET /assets/recorder-theme/", miniapp)

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

	go initializeBot(ctx, cfg, webhook, miniapp)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

type dynamicWebhook struct {
	mu      sync.RWMutex
	handler http.Handler
}

func newDynamicWebhook() *dynamicWebhook {
	return &dynamicWebhook{
		handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bot is starting", http.StatusServiceUnavailable)
		}),
	}
}

func (d *dynamicWebhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	handler := d.handler
	d.mu.RUnlock()
	handler.ServeHTTP(w, r)
}

func (d *dynamicWebhook) Set(handler http.Handler) {
	d.mu.Lock()
	d.handler = handler
	d.mu.Unlock()
}

func initializeBot(ctx context.Context, cfg config.Config, webhook *dynamicWebhook, miniapp *dynamicWebhook) {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("connect database: %v", err)
		return
	}
	go func() {
		<-ctx.Done()
		pool.Close()
	}()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Printf("migrate database: %v", err)
		return
	}

	repo := repositories.New(pool)
	maxClient := maxapi.NewClient(cfg.MaxAPIBaseURL, cfg.MaxBotToken)
	botService := services.NewDatingService(repo, maxClient, cfg.AdminPlatformIDs, cfg.PublicBaseURL, cfg.ReturnToBotURL)
	webhook.Set(handlers.NewWebhookHandler(cfg, botService))
	miniMux := http.NewServeMux()
	handlers.NewMiniAppHandler(cfg, repo, maxClient).Register(miniMux)
	miniapp.Set(miniMux)
	log.Printf("bot services initialized")

	if cfg.MaxBotToken != "" && cfg.PublicBaseURL != "" && cfg.PublicBaseURL != "http://localhost:8080" {
		webhookURL := strings.TrimRight(cfg.PublicBaseURL, "/") + "/webhook/max"
		if err := maxClient.SubscribeWebhook(ctx, webhookURL, cfg.MaxWebhookSecret, []string{"message_created", "message_callback", "bot_started"}); err != nil {
			log.Printf("subscribe max webhook %s: %v", webhookURL, err)
		} else {
			log.Printf("max webhook subscribed: %s", webhookURL)
		}
	}
}
