package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
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
	case "delete":
		err = runDelete(os.Args[2:])
	case "list":
		err = runList(os.Args[2:])
	case "delete-all":
		err = runDeleteAll(os.Args[2:])
	case "restore":
		err = runRestore(os.Args[2:])
	case "system":
		err = runSystem(os.Args[2:])
	case "init-system":
		err = runInitSystem(os.Args[2:])
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
  register    create a new user (--role admin|user, default user;
              --display-name optional)
  login       verify credentials and issue an access token
  delete      soft-delete a user by --id or --email (no permission checks);
              --hard removes permanently including the Ethora mirror
  restore     restore a soft-deleted user by --id or --email
  list        list users (--limit, --offset; no permission checks)
  delete-all  delete ALL users except the system account (requires --yes)
  system      show the system account and its stored XMPP credentials
  init-system create the system account; fails if it already exists

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
	storage, chat, _, cleanup, err := openStorageWithConfig(ctx, configPath)
	return storage, chat, cleanup, err
}

// openStorageWithConfig is openStorage for commands that also need the
// config itself (login signs an access token with auth.jwt_secret).
func openStorageWithConfig(ctx context.Context, configPath string) (*users.Storage, users.ChatBackend, config.Config, func(), error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, config.Config{}, nil, err
	}

	client, err := mongo.Connect(options.Client().ApplyURI(cfg.Mongo.URI))
	if err != nil {
		return nil, nil, config.Config{}, nil, fmt.Errorf("connect to mongo: %w", err)
	}
	cleanup := func() { client.Disconnect(context.Background()) }

	if err := client.Ping(ctx, nil); err != nil {
		cleanup()
		return nil, nil, config.Config{}, nil, fmt.Errorf("ping mongo: %w", err)
	}

	storage, err := users.NewStorage(ctx, client.Database(cfg.Mongo.Database))
	if err != nil {
		cleanup()
		return nil, nil, config.Config{}, nil, fmt.Errorf("init storage: %w", err)
	}

	var chat users.ChatBackend
	if cfg.Ethora.APIKey != "" && cfg.Ethora.APISecret != "" {
		chat = mirror.NewEthora(ethora.NewClient(cfg.Ethora.BaseURL, cfg.Ethora.APIKey, cfg.Ethora.APISecret))
	}
	return storage, chat, cfg, cleanup, nil
}

func runRegister(args []string) error {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	email := fs.String("email", "", "user email (required)")
	password := fs.String("password", "", "user password (required)")
	role := fs.String("role", string(permissions.RoleUser), `role: "admin" or "user"`)
	displayName := fs.String("display-name", "", "human-readable name (optional)")
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

	u, err := users.Register(ctx, storage, chat, users.RegisterParams{
		Email:       *email,
		Password:    *password,
		Role:        permissions.Role(*role),
		DisplayName: *displayName,
	})
	if err != nil {
		return err
	}

	fmt.Printf("user created: id=%s email=%s role=%s display_name=%s\n",
		u.ID.Hex(), u.Email, u.Role, u.DisplayName)
	return nil
}

func runDelete(args []string) error {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	id := fs.String("id", "", "user id in hex")
	email := fs.String("email", "", "user email (alternative to --id)")
	hard := fs.Bool("hard", false, "permanently remove the user and their Ethora mirror (default: soft delete)")
	configPath := fs.String("config", "config.yaml", "path to config file")
	fs.Parse(args)

	if (*id == "") == (*email == "") {
		fs.Usage()
		return fmt.Errorf("exactly one of --id or --email is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancel()

	storage, chat, cleanup, err := openStorage(ctx, *configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	// Any* lookups also find soft-deleted users, so --hard can purge them.
	var u users.User
	if *email != "" {
		u, err = storage.AnyUserByEmail(ctx, *email)
	} else {
		var oid bson.ObjectID
		oid, err = bson.ObjectIDFromHex(*id)
		if err != nil {
			return fmt.Errorf("invalid user id: %q", *id)
		}
		u, err = storage.AnyUserByID(ctx, oid)
	}
	if err != nil {
		return err
	}

	if *hard {
		if err := users.HardDeleteUser(ctx, storage, chat, u.ID); err != nil {
			return err
		}
		fmt.Printf("user permanently deleted: id=%s email=%s\n", u.ID.Hex(), u.Email)
		return nil
	}
	if err := users.DeleteUser(ctx, storage, u.ID); err != nil {
		return err
	}
	fmt.Printf("user soft-deleted: id=%s email=%s\n", u.ID.Hex(), u.Email)
	return nil
}

func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	id := fs.String("id", "", "user id in hex")
	email := fs.String("email", "", "user email (alternative to --id)")
	configPath := fs.String("config", "config.yaml", "path to config file")
	fs.Parse(args)

	if (*id == "") == (*email == "") {
		fs.Usage()
		return fmt.Errorf("exactly one of --id or --email is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancel()

	storage, _, cleanup, err := openStorage(ctx, *configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	oid := bson.ObjectID{}
	if *email != "" {
		u, err := storage.AnyUserByEmail(ctx, *email)
		if err != nil {
			return err
		}
		oid = u.ID
	} else {
		oid, err = bson.ObjectIDFromHex(*id)
		if err != nil {
			return fmt.Errorf("invalid user id: %q", *id)
		}
	}

	u, err := users.RestoreUser(ctx, storage, oid)
	if err != nil {
		return err
	}
	fmt.Printf("user restored: id=%s email=%s\n", u.ID.Hex(), u.Email)
	return nil
}

func runSystem(args []string) error {
	fs := flag.NewFlagSet("system", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to config file")
	fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancel()

	storage, _, cleanup, err := openStorage(ctx, *configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	u, err := storage.AnyUserByEmail(ctx, users.SystemEmail)
	if errors.Is(err, users.ErrNotFound) {
		return fmt.Errorf("system account does not exist yet — start the server once to create it")
	}
	if err != nil {
		return err
	}

	fmt.Printf("id=%s\n", u.ID.Hex())
	fmt.Printf("email=%s\n", u.Email)
	fmt.Printf("role=%s\n", u.Role)
	fmt.Printf("created_at=%s\n", u.CreatedAt.Format(time.RFC3339))
	if u.DeletedAt != nil {
		fmt.Printf("deleted_at=%s\n", u.DeletedAt.Format(time.RFC3339))
	}

	creds, err := storage.ChatCredentialsByUserID(ctx, u.ID)
	switch {
	case errors.Is(err, users.ErrNotFound):
		fmt.Println("xmpp_credentials=none")
	case err != nil:
		return err
	default:
		fmt.Printf("xmpp_username=%s\n", creds.XMPPUsername)
		fmt.Printf("xmpp_password=%s\n", creds.XMPPPassword)
		fmt.Printf("xmpp_updated_at=%s\n", creds.UpdatedAt.Format(time.RFC3339))
	}
	return nil
}

func runInitSystem(args []string) error {
	fs := flag.NewFlagSet("init-system", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to config file")
	fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancel()

	storage, chat, cleanup, err := openStorage(ctx, *configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	u, err := users.InitSystemUser(ctx, storage, chat)
	if err != nil {
		return err
	}
	fmt.Printf("system user created: id=%s email=%s\n", u.ID.Hex(), u.Email)
	return nil
}

func runDeleteAll(args []string) error {
	fs := flag.NewFlagSet("delete-all", flag.ExitOnError)
	yes := fs.Bool("yes", false, "confirm deleting ALL users (required)")
	configPath := fs.String("config", "config.yaml", "path to config file")
	fs.Parse(args)

	if !*yes {
		fs.Usage()
		return fmt.Errorf("refusing to delete all users without --yes")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancel()

	storage, chat, cleanup, err := openStorage(ctx, *configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	count, err := users.DeleteAllUsers(ctx, storage, chat)
	if err != nil {
		return err
	}
	fmt.Printf("deleted %d users\n", count)
	return nil
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	limit := fs.Int64("limit", 50, "maximum users to print")
	offset := fs.Int64("offset", 0, "users to skip")
	configPath := fs.String("config", "config.yaml", "path to config file")
	fs.Parse(args)

	if *limit < 1 || *offset < 0 {
		fs.Usage()
		return fmt.Errorf("--limit must be >= 1 and --offset >= 0")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancel()

	storage, _, cleanup, err := openStorage(ctx, *configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	list, err := storage.ListUsers(ctx, *limit, *offset)
	if err != nil {
		return err
	}
	for _, u := range list {
		// New columns go at the end: scripts parse this by field index.
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n",
			u.ID.Hex(), u.Role, u.CreatedAt.Format(time.RFC3339), u.Email, u.DisplayName)
	}
	return nil
}

func runLogin(args []string) error {
	email, password, configPath, err := credentialFlags("login", args)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
	defer cancel()

	storage, _, cfg, cleanup, err := openStorageWithConfig(ctx, configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	u, err := users.Login(ctx, storage, email, password)
	if err != nil {
		return err
	}
	if cfg.Auth.JWTSecret == "" {
		return fmt.Errorf("auth.jwt_secret is not configured (set it in the config file or JWT_SECRET)")
	}
	token, err := users.IssueToken(u, []byte(cfg.Auth.JWTSecret), users.TokenTTL)
	if err != nil {
		return err
	}

	// Same payload the HTTP login returns, for scripts that parse it.
	fmt.Printf("login ok: id=%s email=%s\n", u.ID.Hex(), u.Email)
	fmt.Printf("access_token=%s\n", token)
	fmt.Printf("expires_at=%s\n", time.Now().UTC().Add(users.TokenTTL).Format(time.RFC3339))

	switch creds, err := storage.ChatCredentialsByUserID(ctx, u.ID); {
	case err == nil:
		fmt.Printf("xmpp_username=%s\n", creds.XMPPUsername)
		fmt.Printf("xmpp_password=%s\n", creds.XMPPPassword)
	case !errors.Is(err, users.ErrNotFound):
		return err
	}
	return nil
}
