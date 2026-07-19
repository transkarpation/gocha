package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// splitAndTrim splits a comma-separated list, dropping blank entries and
// surrounding whitespace (used for CORS_ALLOWED_ORIGINS).
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type Config struct {
	Server ServerConfig `yaml:"server"`
	Mongo  MongoConfig  `yaml:"mongo"`
	Ethora EthoraConfig `yaml:"ethora"`
	Auth   AuthConfig   `yaml:"auth"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
	// AllowedOrigins are the browser origins CORS lets call the API directly
	// (so the SPA needs no dev proxy). A single "*" entry allows any origin
	// (without credentials); an empty list disables CORS.
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// AuthConfig holds our own signing material. JWTSecret deliberately has no
// default: an empty HS256 key would make user tokens forgeable, so the
// server refuses to start without it.
type AuthConfig struct {
	JWTSecret string `yaml:"jwt_secret"`
}

type MongoConfig struct {
	URI      string `yaml:"uri"`
	Database string `yaml:"database"`
}

type EthoraConfig struct {
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	APISecret string `yaml:"api_secret"`
	XMPPWSURL string `yaml:"xmpp_ws_url"`
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Port: 8080,
			// Vite dev server defaults; override for other hosts.
			AllowedOrigins: []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		},
		Mongo: MongoConfig{
			URI:      "mongodb://root:example@localhost:27017",
			Database: "protected_server",
		},
		Ethora: EthoraConfig{
			BaseURL:   "https://api.chat.ethora.com/",
			XMPPWSURL: "wss://xmpp.chat.ethora.com/ws",
		},
	}
}

// Load reads the config file and applies env overrides
// (APP_PORT, MONGO_URI, MONGO_DB, ETHORA_BASE_URL, ETHORA_API_KEY,
// ETHORA_API_SECRET, ETHORA_XMPP_WS_URL, JWT_SECRET,
// CORS_ALLOWED_ORIGINS) on top of it.
// A missing file is not an error — defaults are used instead.
func Load(path string) (Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		// Keep defaults — a missing config file is legitimate. Say so
		// with the absolute path though: `path` is relative to the
		// working directory, so running the binary from bin/ silently
		// looks for bin/config.yaml and falls back to defaults that have
		// no jwt_secret. Without this line the only symptom is a
		// confusing "auth.jwt_secret is not configured" at startup.
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			abs = path
		}
		slog.Warn("config file not found, using defaults", "path", abs)
	case err != nil:
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	default:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	if v := os.Getenv("APP_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid APP_PORT %q: %w", v, err)
		}
		cfg.Server.Port = port
	}
	if v := os.Getenv("MONGO_URI"); v != "" {
		cfg.Mongo.URI = v
	}
	if v := os.Getenv("MONGO_DB"); v != "" {
		cfg.Mongo.Database = v
	}
	if v := os.Getenv("ETHORA_BASE_URL"); v != "" {
		cfg.Ethora.BaseURL = v
	}
	if v := os.Getenv("ETHORA_API_KEY"); v != "" {
		cfg.Ethora.APIKey = v
	}
	if v := os.Getenv("ETHORA_API_SECRET"); v != "" {
		cfg.Ethora.APISecret = v
	}
	if v := os.Getenv("ETHORA_XMPP_WS_URL"); v != "" {
		cfg.Ethora.XMPPWSURL = v
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.Auth.JWTSecret = v
	}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		cfg.Server.AllowedOrigins = splitAndTrim(v)
	}

	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return Config{}, fmt.Errorf("invalid server port: %d", cfg.Server.Port)
	}
	return cfg, nil
}
