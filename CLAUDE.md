# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
# MongoDB must be running before the server or CLI will start
docker compose up -d

# Build both binaries into bin/ (gitignored; module requires Go >= 1.23,
# toolchain auto-downloads)
go build -o bin/gocha.exe .
go build -o bin/backendctrl.exe ./cmd/backendctrl

# Run the server (flags: -config <path>, default config.yaml)
go run .

# CLI: create a user / issue a session token
./bin/backendctrl.exe register --email a@b.com --password secret123
./bin/backendctrl.exe login --email a@b.com --password secret123

# Run tests (users/chats tests are integration tests — they need the
# docker-compose Mongo running, and SKIP themselves if it is not)
go test ./...

# Single test
go test ./internal/chats -run TestMessages/limit_and_offset
```

Each integration test gets a unique throwaway database via
`testutil.MongoDB(t)` (dropped in cleanup), so tests are parallel-safe and
never touch the `protected_server` database.

## Configuration

Priority: env vars > `config.yaml` > defaults in `internal/config/config.go`.
Env overrides: `APP_PORT`, `MONGO_URI`, `MONGO_DB`. A missing config file is
not an error (defaults match the docker-compose Mongo: root/example@localhost:27017,
db `protected_server`, port 8080). Both binaries take a `-config`/`--config` flag.

## Architecture

Two entry points share the `internal/` packages:

- `main.go` — HTTP server (chi). `setupRouter` wires all routes; protected
  routes live in the `r.Group` that applies `users.Handler.Auth`.
- `cmd/backendctrl/main.go` — admin CLI (`register`, `login` subcommands).
  Its results (`session_token=...`) print via `fmt` to stdout on purpose —
  scripts parse them; don't convert to slog.

Layering inside `internal/users` (and mirrored in `internal/chats`):

- `storage.go` — Mongo collections and queries only. Typed sentinel errors
  (`ErrNotFound`, `ErrEmailTaken`) instead of driver errors.
- `service.go` — business logic shared by HTTP and CLI (`Register`, `Login`,
  `IssueSession`): validation, bcrypt hashing, token generation. New logic
  used by both entry points belongs here, not in handlers.
- `handler.go` / `middleware.go` — HTTP layer: decode JSON, call service,
  map sentinel errors to status codes (422 validation, 409 conflict,
  401 credentials/session, 500 with slog.ErrorContext).

Roles and permissions (`internal/permissions`): the single registry of roles
(`admin`, `user`) and per-entity permissions (`chats:create`, ...). **Every new
entity exposed over HTTP must define its permission set in this package and
guard its routes with `users.RequirePermission(...)` (applied after `Auth` via
`r.With`).** HTTP self-registration always creates role `user`; admins are
created only via `backendctrl register --role admin`. Users stored before roles
existed have no `role` field — storage normalizes that to `user` on read, and
`permissions.Has` treats empty role as `user` too.

Sessions: opaque random tokens (not JWT) stored in the `sessions` collection.
`Auth` middleware accepts `Authorization: Bearer <token>` (priority) or the
`session` HttpOnly cookie, loads the user and stores it in the request context;
handlers read it with `users.FromContext(ctx)`. Session expiry is enforced in
the `SessionByToken` query (`expires_at > now`) — the Mongo TTL index cleans up
lazily and must not be relied on for correctness.

Collections and their invariants (created in `users.NewStorage`):
`users` — unique index on `email`; `sessions` — TTL index on `expires_at`;
`chats` — participants are validated against `users` at creation time and the
creator from the session is always included.

Startup: `slog.SetDefault` in `main` configures the text logger all packages
use. Server shutdown is graceful (signal.NotifyContext + srv.Shutdown).

## Notes

- `task.txt` is a spec for a licensing system (Centrifugo PRO-style) that has
  not been implemented yet — do not treat it as a description of current code.
- Windows dev environment: Git Bash `kill` cannot deliver Ctrl+C to the native
  server binary (it hard-kills). To exercise graceful shutdown, send
  CTRL_BREAK_EVENT via WinAPI to a process started with CREATE_NEW_PROCESS_GROUP.
