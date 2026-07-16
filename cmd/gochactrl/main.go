package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/transkarpation/gocha/internal/config"
	"github.com/transkarpation/gocha/internal/mirror"
	"github.com/transkarpation/gocha/internal/permissions"
	"github.com/transkarpation/gocha/internal/users"
	"github.com/transkarpation/gocha/pkg/ethora"
)

const mongoTimeout = 5 * time.Second

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "register":
		err = runRegister(os.Args[2:])
	case "login":
		err = runLogin(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: gochactrl <command> [flags]

Commands:
  register   create a new user (--role admin|user, default user)
  login      verify credentials and issue a session token

Common flags:
  --email <addr> --password <pass> [--config <path>]

Configuration:
  read from config.yaml (override path with --config),
  env MONGO_URI / MONGO_DB take precedence`)
}

func credentialFlags(name string, args []string) (email, password, configPath string, err error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	emailF := fs.String("email", "", "user email (required)")
	passwordF := fs.String("password", "", "user password (required)")
	configF := fs.String("config", "config.yaml", "path to config file")
	fs.Parse(args)

	if *emailF == "" || *passwordF == "" {
		fs.Usage()
		return "", "", "", fmt.Errorf("--email and --password are required")
	}
	return *emailF, *passwordF, *configF, nil
}

// openStorage connects to Mongo from the config file and returns the users
// storage, the external chat mirror (nil when Ethora credentials are not
// configured) and a cleanup func that disconnects the client.
func openStorage(ctx context.Context, configPath string) (*users.Storage, users.ChatBackend, func(), error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, nil, err
	}

	client, err := mongo.Connect(options.Client().ApplyURI(cfg.Mongo.URI))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("connect to mongo: %w", err)
	}
	cleanup := func() { client.Disconnect(context.Background()) }

	if err := client.Ping(ctx, nil); err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("ping mongo: %w", err)
	}

	storage, err := users.NewStorage(ctx, client.Database(cfg.Mongo.Database))
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("init storage: %w", err)
	}

	var chat users.ChatBackend
	if cfg.Ethora.APIKey != "" && cfg.Ethora.APISecret != "" {
		chat = mirror.NewEthora(ethora.NewClient(cfg.Ethora.BaseURL, cfg.Ethora.APIKey, cfg.Ethora.APISecret))
	}
	return storage, chat, cleanup, nil
}

func runRegister(args []string) error {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	email := fs.String("email", "", "user email (required)")
	password := fs.String("password", "", "user password (required)")
	role := fs.String("role", string(permissions.RoleUser), `role: "admin" or "user"`)
	configPath := fs.String("config", "config.yaml", "path to config file")
	fs.Parse(args)

	if *email == "" || *password == "" {
		fs.Usage()
		return fmt.Errorf("--email and --password are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancel()

	storage, chat, cleanup, err := openStorage(ctx, *configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	u, err := users.Register(ctx, storage, chat, *email, *password, permissions.Role(*role))
	if err != nil {
		return err
	}

	fmt.Printf("user created: id=%s email=%s role=%s\n", u.ID.Hex(), u.Email, u.Role)
	return nil
}

func runLogin(args []string) error {
	email, password, configPath, err := credentialFlags("login", args)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancel()

	storage, _, cleanup, err := openStorage(ctx, configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	u, err := users.Login(ctx, storage, email, password)
	if err != nil {
		return err
	}
	sess, err := users.IssueSession(ctx, storage, u)
	if err != nil {
		return err
	}

	fmt.Printf("login ok: id=%s email=%s\n", u.ID.Hex(), u.Email)
	fmt.Printf("session_token=%s\n", sess.Token)
	fmt.Printf("expires_at=%s\n", sess.ExpiresAt.Format(time.RFC3339))
	return nil
}
