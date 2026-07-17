# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
# Common workflows are wrapped in a Taskfile (https://taskfile.dev;
# install: go install github.com/go-task/task/v3/cmd/task@latest)
task up      # docker compose up -d — Mongo must run before server/CLI start
task build   # build both binaries into bin/ (incremental via checksums
             # in gitignored .task/; module requires Go >= 1.23)
task run     # run the server (flags: -config <path>, default config.yaml)
task test    # go test ./...
task clean   # remove bin/

# The raw commands behind the tasks also work directly:
go build -o bin/gocha.exe .
go build -o bin/gochactrl.exe ./cmd/gochactrl
go run .

# CLI: create a user / issue a session token
./bin/gochactrl.exe register --email a@b.com --password secret123
./bin/gochactrl.exe login --email a@b.com --password secret123

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
Env overrides: `APP_PORT`, `MONGO_URI`, `MONGO_DB`. `config.yaml` is gitignored —
copy it from the tracked `config.example.yaml`. A missing config file is
not an error (defaults match the docker-compose Mongo: root/example@localhost:27017,
db `protected_server`, port 8080). Both binaries take a `-config`/`--config` flag.

## Architecture

`pkg/ethora` is a standalone HTTP client for the Ethora chat platform API:
every request carries a JWT signed HS256 with the API secret, claims MUST be
nested as `{"data": {"type": "server", "appId": <api key>}}` — top-level
claims get rejected as INVALID_TOKEN_TYPE (raw token in the Authorization
header, no Bearer prefix). Batch user creation uses the synchronous v1
endpoint (200 + created users in `results`; the v2 variant is asynchronous —
202 + jobId). The client logs every request and response at Info (method,
path, status, body) with password-carrying values (`password`,
`tempPassword`, `xmppPassword`, ...) masked — one-off mirror passwords must
never reach logs.

User mirroring: `users.Register` calls the `users.ChatBackend` interface when
one is provided; `internal/mirror.Ethora` is the adapter (random names,
one-off password, our user id as uuid). Mirroring is best-effort — a failure
is logged, registration still succeeds; that policy lives in `users.Register`.
`MirrorUser` returns a `users.ChatAccount` (the XMPP credentials Ethora
generated); `Register` persists it in the `chat_credentials` collection
(keyed by user id — deliberately NOT on the User document, so backend-specific
secrets never travel with User values through handlers/listings). Credentials
survive a soft delete, are removed on hard delete / delete-all, and are read
back via `Storage.ChatCredentialsByUserID`.
Credentials and base_url come from the `ethora` config section; without
credentials mirroring is disabled (nil backend). Ethora's batch delete is
all-or-nothing (404 when any id is unknown), so the adapter falls back to
per-id deletes and treats a single-id 404 as success (already gone).

Two entry points share the `internal/` packages:

- `main.go` — HTTP server (chi). `setupRouter` wires all routes; protected
  routes live in the `r.Group` that applies `users.Handler.Auth`.
- `cmd/gochactrl/main.go` — admin CLI (`register`, `login`, `delete`,
  `restore`, `list`, `delete-all`, `system` subcommands; all except `login`
  bypass permission checks by design, `delete-all` requires `--yes`,
  `system` prints the system account incl. its XMPP credentials). There is
  deliberately no HTTP route for bulk deletion.
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

User deletion is soft by default: `users.DeleteUser` sets `deleted_at`, kills
sessions and deliberately does NOT touch the Ethora mirror. All read paths
(`UserByEmail/ID`, `ListUsers`, `CountExisting`, `UpdateUser`) filter
soft-deleted users out via the `notDeleted` clause — a soft-deleted user's
email stays taken (unique index still sees the document). Permanent removal
(`users.HardDeleteUser`, incl. Ethora) exists only behind
`gochactrl delete --hard`; the `Any*` storage lookups bypass the filter so
`--hard` can purge already-soft-deleted users. `delete-all` is always hard.
Soft-deleted users can be restored (`POST /users/{id}/restore` under
`users:update`, or `gochactrl restore`) — sessions are not resurrected,
restoring an alive user is ErrNotDeleted (409).

System account: server startup calls `users.EnsureSystemUser` — an
idempotent ensure of `users.SystemEmail` (`system@gocha.internal`), the
account service messages are sent from. Created with a thrown-away random
password (login impossible; the server acts via storage, not sessions),
mirrored to Ethora like any user, restored automatically if soft-deleted.
After `delete-all` it reappears on the next server start.

Roles and permissions (`internal/permissions`): the single registry of roles
(`admin`, `user`) and per-entity permissions (`chats:create`, ...). **Every new
entity exposed over HTTP must define its permission set in this package and
guard its routes with `users.RequirePermission(...)` (applied after `Auth` via
`r.With`).** HTTP self-registration always creates role `user`; admins are
created only via `gochactrl register --role admin`. Users stored before roles
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
`chat_credentials` — XMPP credentials of the mirrored account, `_id` = user id;
`chats` — participants are validated against `users` at creation time and the
creator from the session is always included.

Startup: `slog.SetDefault` in `main` configures the text logger all packages
use. Server shutdown is graceful (signal.NotifyContext + srv.Shutdown).

## Notes

- Windows dev environment: Git Bash `kill` cannot deliver Ctrl+C to the native
  server binary (it hard-kills). To exercise graceful shutdown, send
  CTRL_BREAK_EVENT via WinAPI to a process started with CREATE_NEW_PROCESS_GROUP.
