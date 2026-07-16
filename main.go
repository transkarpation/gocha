package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/transkarpation/gocha/internal/chats"
	"github.com/transkarpation/gocha/internal/config"
	"github.com/transkarpation/gocha/internal/mirror"
	"github.com/transkarpation/gocha/internal/permissions"
	"github.com/transkarpation/gocha/internal/users"
	"github.com/transkarpation/gocha/pkg/ethora"
)

const (
	middlewareTimeout = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
	mongoTimeout      = 5 * time.Second
)

// chatBackend builds the external chat mirror from config; returns nil
// (mirroring disabled) when Ethora credentials are not set.
func chatBackend(cfg config.EthoraConfig) users.ChatBackend {
	if cfg.APIKey == "" || cfg.APISecret == "" {
		slog.Info("ethora mirroring disabled: no credentials configured")
		return nil
	}
	slog.Info("ethora mirroring enabled", "base_url", cfg.BaseURL)
	return mirror.NewEthora(ethora.NewClient(cfg.BaseURL, cfg.APIKey, cfg.APISecret))
}

func setupRouter(storage *users.Storage, chatStorage *chats.Storage, chat users.ChatBackend) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(middlewareTimeout))

	r.Get("/healthcheck", handleHealthcheck)

	h := users.NewHandler(storage, chat)
	r.Post("/register", h.Register)
	r.Post("/login", h.Login)

	ch := chats.NewHandler(chatStorage, storage)

	r.Group(func(r chi.Router) {
		r.Use(h.Auth)
		r.Get("/me", h.Me)
		r.With(users.RequirePermission(permissions.UsersRead)).Get("/users", h.List)
		r.With(users.RequirePermission(permissions.UsersUpdate)).Patch("/users/{id}", h.Update)
		r.With(users.RequirePermission(permissions.UsersDelete)).Delete("/users/{id}", h.Delete)
		r.With(users.RequirePermission(permissions.ChatsCreate)).Post("/chats", ch.Create)
		r.With(users.RequirePermission(permissions.ChatsDelete)).Delete("/chats/{id}", ch.Delete)
		r.With(users.RequirePermission(permissions.MessagesCreate)).Post("/chats/{id}/messages", ch.SendMessage)
		r.With(users.RequirePermission(permissions.MessagesRead)).Get("/chats/{id}/messages", ch.ListMessages)
	})

	return r
}

func handleHealthcheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	client, err := mongo.Connect(options.Client().ApplyURI(cfg.Mongo.URI))
	if err != nil {
		slog.Error("connect to mongo", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := client.Disconnect(context.Background()); err != nil {
			slog.Error("disconnect mongo", "error", err)
		}
	}()

	mongoCtx, cancelMongo := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancelMongo()
	if err := client.Ping(mongoCtx, nil); err != nil {
		slog.Error("ping mongo", "error", err)
		os.Exit(1)
	}

	storage, err := users.NewStorage(mongoCtx, client.Database(cfg.Mongo.Database))
	if err != nil {
		slog.Error("init users storage", "error", err)
		os.Exit(1)
	}

	chatStorage, err := chats.NewStorage(mongoCtx, client.Database(cfg.Mongo.Database))
	if err != nil {
		slog.Error("init chats storage", "error", err)
		os.Exit(1)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: setupRouter(storage, chatStorage, chatBackend(cfg.Ethora)),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen and serve", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down gracefully")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
	slog.Info("server stopped")
}
