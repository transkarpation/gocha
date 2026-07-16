package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("port = %d, want default 8080", cfg.Server.Port)
	}
	if cfg.Mongo.Database != "protected_server" {
		t.Errorf("database = %q, want default", cfg.Mongo.Database)
	}
	if cfg.Ethora.BaseURL != "https://api.chat.ethora.com/" {
		t.Errorf("ethora base_url = %q, want default", cfg.Ethora.BaseURL)
	}
}

func TestLoadFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := `
server:
  port: 9090
mongo:
  uri: mongodb://example:27017
  database: testdb
ethora:
  api_key: file-key
  api_secret: file-secret
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 9090 || cfg.Mongo.URI != "mongodb://example:27017" || cfg.Mongo.Database != "testdb" {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if cfg.Ethora.APIKey != "file-key" || cfg.Ethora.APISecret != "file-secret" {
		t.Errorf("ethora = %+v, want values from file", cfg.Ethora)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("APP_PORT", "7070")
	t.Setenv("MONGO_URI", "mongodb://env:27017")
	t.Setenv("MONGO_DB", "envdb")
	t.Setenv("ETHORA_BASE_URL", "https://env.example.com/")
	t.Setenv("ETHORA_API_KEY", "env-key")
	t.Setenv("ETHORA_API_SECRET", "env-secret")

	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 7070 || cfg.Mongo.URI != "mongodb://env:27017" || cfg.Mongo.Database != "envdb" {
		t.Errorf("env overrides not applied: %+v", cfg)
	}
	if cfg.Ethora.BaseURL != "https://env.example.com/" || cfg.Ethora.APIKey != "env-key" || cfg.Ethora.APISecret != "env-secret" {
		t.Errorf("ethora env overrides not applied: %+v", cfg.Ethora)
	}
}

func TestInvalidValues(t *testing.T) {
	t.Run("bad APP_PORT", func(t *testing.T) {
		t.Setenv("APP_PORT", "not-a-number")
		if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
			t.Error("expected error for invalid APP_PORT")
		}
	})
	t.Run("port out of range", func(t *testing.T) {
		t.Setenv("APP_PORT", "70000")
		if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
			t.Error("expected error for out-of-range port")
		}
	})
	t.Run("broken yaml", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		os.WriteFile(path, []byte("server: [broken"), 0o600)
		if _, err := Load(path); err == nil {
			t.Error("expected error for broken yaml")
		}
	})
}
