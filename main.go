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
	"github.com/transkarpation/gocha/internal/xmppclient"
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

func setupRouter(storage *users.Storage, chatStorage *chats.Storage, chat users.ChatBackend, jwtSecret []byte, allowedOrigins []string) chi.Router {
	r := chi.NewRouter()

	// CORS first so preflight requests are answered before any other work and
	// the headers are present even on panics further down the chain.
	if len(allowedOrigins) > 0 {
		r.Use(corsMiddleware(allowedOrigins))
	}
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(middlewareTimeout))

	r.Get("/healthcheck", handleHealthcheck)

	h := users.NewHandler(storage, chat, jwtSecret)
	r.Post("/register", h.Register)
	r.Post("/login", h.Login)

	ch := chats.NewHandler(chatStorage, storage)

	r.Group(func(r chi.Router) {
		r.Use(h.Auth)
		r.Get("/me", h.Me)
		r.With(users.RequirePermission(permissions.UsersRead)).Get("/users", h.List)
		r.With(users.RequirePermission(permissions.UsersUpdate)).Patch("/users/{id}", h.Update)
		r.With(users.RequirePermission(permissions.UsersUpdate)).Post("/users/{id}/restore", h.Restore)
		r.With(users.RequirePermission(permissions.UsersDelete)).Delete("/users/{id}", h.Delete)
		r.With(users.RequirePermission(permissions.ChatsCreate)).Post("/chats", ch.Create)
		r.With(users.RequirePermission(permissions.ChatsDelete)).Delete("/chats/{id}", ch.Delete)
		r.With(users.RequirePermission(permissions.MessagesCreate)).Post("/chats/{id}/messages", ch.SendMessage)
		r.With(users.RequirePermission(permissions.MessagesRead)).Get("/chats/{id}/messages", ch.ListMessages)
	})

	return r
}

// startSystemXMPP connects the system account to the XMPP server in its
// own goroutine (skipped when the endpoint is not configured or the system
// account has no stored credentials — e.g. mirroring is disabled).
func startSystemXMPP(ctx context.Context, wsURL string, storage *users.Storage, sysUser users.User) {
	if wsURL == "" {
		slog.Info("xmpp connection disabled: no websocket url configured")
		return
	}
	lookupCtx, cancel := context.WithTimeout(ctx, mongoTimeout)
	defer cancel()
	creds, err := storage.ChatCredentialsByUserID(lookupCtx, sysUser.ID)
	if err != nil {
		slog.Warn("xmpp connection disabled: no chat credentials for system user", "error", err)
		return
	}
	go func() {
		if err := xmppclient.Run(ctx, wsURL, creds.XMPPUsername, creds.XMPPPassword); err != nil {
			slog.Error("xmpp client stopped", "error", err)
		}
	}()
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

	// Signing user tokens with an empty key would make them forgeable.
	if cfg.Auth.JWTSecret == "" {
		slog.Error("auth.jwt_secret is not configured (set it in the config file or JWT_SECRET)",
			"config", *configPath)
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

	chat := chatBackend(cfg.Ethora)

	// The server's own account: service messages are sent on its behalf.
	sysUser, err := users.EnsureSystemUser(mongoCtx, storage, chat)
	if err != nil {
		slog.Error("ensure system user", "error", err)
		os.Exit(1)
	}
	slog.Info("system user ready", "user_id", sysUser.ID.Hex(), "email", sysUser.Email)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: setupRouter(storage, chatStorage, chat, []byte(cfg.Auth.JWTSecret), cfg.Server.AllowedOrigins),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startSystemXMPP(ctx, cfg.Ethora.XMPPWSURL, storage, sysUser)

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
