package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	Mongo  MongoConfig  `yaml:"mongo"`
	Ethora EthoraConfig `yaml:"ethora"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type MongoConfig struct {
	URI      string `yaml:"uri"`
	Database string `yaml:"database"`
}

type EthoraConfig struct {
	APIKey    string `yaml:"api_key"`
	APISecret string `yaml:"api_secret"`
}

func defaults() Config {
	return Config{
		Server: ServerConfig{Port: 8080},
		Mongo: MongoConfig{
			URI:      "mongodb://root:example@localhost:27017",
			Database: "protected_server",
		},
	}
}

// Load reads the config file and applies env overrides
// (APP_PORT, MONGO_URI, MONGO_DB, ETHORA_API_KEY, ETHORA_API_SECRET)
// on top of it. A missing file is not an error — defaults are used instead.
func Load(path string) (Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		// keep defaults
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
	if v := os.Getenv("ETHORA_API_KEY"); v != "" {
		cfg.Ethora.APIKey = v
	}
	if v := os.Getenv("ETHORA_API_SECRET"); v != "" {
		cfg.Ethora.APISecret = v
	}

	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return Config{}, fmt.Errorf("invalid server port: %d", cfg.Server.Port)
	}
	return cfg, nil
}
