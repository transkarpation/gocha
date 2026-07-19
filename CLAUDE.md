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

# CLI: create a user / issue an access token
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
Env overrides: `APP_PORT`, `MONGO_URI`, `MONGO_DB`, `CORS_ALLOWED_ORIGINS`
(comma-separated). `config.yaml` is gitignored —
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
  routes live in the `r.Group` that applies `users.Handler.Auth`. When
  `server.allowed_origins` is non-empty it installs `corsMiddleware`
  (`cors.go`) as the first middleware so the SPA can call the API
  cross-origin without a dev proxy — a listed origin is reflected with
  credentials allowed, `"*"` allows any origin without credentials, and
  preflight `OPTIONS` is answered there. The default allows the Vite dev
  server (`http://localhost:5173`).
- `cmd/gochactrl/main.go` — admin CLI (`register`, `login`, `delete`,
  `restore`, `list`, `delete-all`, `system`, `init-system` subcommands; all
  except `login` bypass permission checks by design, `delete-all` requires
  `--yes`, `system` prints the system account incl. its XMPP credentials,
  `init-system` creates it and errors with ErrSystemExists when it is
  already there). There is deliberately no HTTP route for bulk deletion.
  Its results (`access_token=...`) print via `fmt` to stdout on purpose —
  scripts parse them; don't convert to slog.

Layering inside `internal/users` (and mirrored in `internal/chats`):

- `storage.go` — Mongo collections and queries only. Typed sentinel errors
  (`ErrNotFound`, `ErrEmailTaken`) instead of driver errors.
- `service.go` — business logic shared by HTTP and CLI (`Register`, `Login`,
  `UpdateUser`): validation, bcrypt hashing, mirroring policy. New logic
  used by both entry points belongs here, not in handlers.
- `handler.go` / `middleware.go` — HTTP layer: decode JSON, validate the
  payload (see below), call service, map sentinel errors to status codes
  (422 validation, 409 conflict, 401 credentials/token, 500 with
  slog.ErrorContext). `userResponse` is the single place deciding which user
  fields leave the handler.

Request validation (`internal/validate`, go-playground/validator/v10):
handlers decode, then `if validate.WriteError(w, validate.Struct(req))
{ return }`, which answers 422 with `{"error": "<summary>", "fields":
{"<json name>": "<message>"}}` — `error` stays a plain string because the
SPA's `errorMessage()` reads it; `fields` is additive and the SPA's
`fieldErrors()` renders it under the matching input. Errors are keyed by the
**JSON** field name (`RegisterTagNameFunc`), never the Go one.
This does **not** replace the service-layer validation: `gochactrl` calls
the service directly and never passes through a handler, so the service is
what guarantees the invariant and validator is the outer gate that rejects
malformed input early with a per-field message. Struct tags cannot reference
Go constants, so the duplicated limits are pinned by
`TestTagsMatchServiceLimits` / `TestTagsMatchPackageLimits` — they fail the
build when a tag and its constant drift apart. Trim before validating
(`required` accepts `"   "`). `loginRequest` deliberately constrains the
password to `required` only: a bad password must come back 401 from the
credential check, not 422 revealing the password policy.

`User.DisplayName` (`display_name`) is the human-readable name. It is
**optional everywhere on the API** — `gochactrl register` (`--display-name`),
the system account and every account predating the field may have none — so
handlers must cope with the empty string. Only the SPA's registration form
insists on one. The service trims it and caps it at `maxDisplayNameLen` runes
(`ErrDisplayNameTooLong` → 422); `PATCH /users/{id}` with an explicit `""`
clears it. `Register` takes `RegisterParams` rather than positional
arguments so callers cannot swap two strings unnoticed. The Ethora mirror
maps it onto the mandatory firstName/lastName pair (`splitDisplayName`,
`internal/mirror`) instead of the random `user_<hex>` placeholder.

User deletion is soft by default: `users.DeleteUser` sets `deleted_at` (which
also locks out the user's access tokens, since Auth resolves every token
through `UserByID`) and deliberately does NOT touch the Ethora mirror.
All read paths
(`UserByEmail/ID`, `ListUsers`, `CountExisting`, `UpdateUser`) filter
soft-deleted users out via the `notDeleted` clause — a soft-deleted user's
email stays taken (unique index still sees the document). Permanent removal
(`users.HardDeleteUser`, incl. Ethora) exists only behind
`gochactrl delete --hard`; the `Any*` storage lookups bypass the filter so
`--hard` can purge already-soft-deleted users. `delete-all` is always hard.
Soft-deleted users can be restored (`POST /users/{id}/restore` under
`users:update`, or `gochactrl restore`) — unexpired tokens issued before the
deletion work again, restoring an alive user is ErrNotDeleted (409).

System account: server startup calls `users.EnsureSystemUser` — an
idempotent ensure of `users.SystemEmail` (`system@gocha.internal`), the
account service messages are sent from. Created with a thrown-away random
password (login impossible; the server acts via storage, not tokens),
mirrored to Ethora like any user, restored automatically if soft-deleted.
`delete-all` never touches it (nor its chat credentials or Ethora mirror);
only a targeted `gochactrl delete --email ... --hard` removes it.
`users.InitSystemUser` (CLI `init-system`) is the non-idempotent variant:
create or fail with ErrSystemExists.

XMPP: after startup the system account connects to the Ethora XMPP server
over websocket (`internal/xmppclient`, library gosrc.io/xmpp — picked for
its built-in RFC 7395 websocket transport and reconnecting StreamManager)
in its own goroutine, authenticating with the stored chat credentials; the
JID is `<xmpp_username>@<host of ethora.xmpp_ws_url>`. Every (re)connect is
logged. Skipped with a log line when `ethora.xmpp_ws_url` is empty or the
system account has no credentials; the goroutine stops via the same signal
context that drives graceful shutdown.

Roles and permissions (`internal/permissions`): the single registry of roles
(`admin`, `user`) and per-entity permissions (`chats:create`, ...). **Every new
entity exposed over HTTP must define its permission set in this package and
guard its routes with `users.RequirePermission(...)` (applied after `Auth` via
`r.With`).** HTTP self-registration always creates role `user`; admins are
created only via `gochactrl register --role admin`. Users stored before roles
existed have no `role` field — storage normalizes that to `user` on read, and
`permissions.Has` treats empty role as `user` too.

Authentication is JWT-only: there are no sessions and no `sessions`
collection (both removed), so nothing is stored server-side per login.

Sign-in payload: `Register`/`Login` (HTTP and `gochactrl login`) hand the
client an access token and the XMPP credentials of its mirrored account
(`xmpp_username`/`xmpp_password`, omitted when the user has no mirror — that
must never break signing in). The token is signed HS256 with
`auth.jwt_secret` (env `JWT_SECRET`; claims sub/email/role/ver/iat/exp, TTL
`users.TokenTTL`) — deliberately our own key, NOT `ethora.api_secret`: never
reuse a third party's credential as our signing key. The secret has no
default and `main` refuses to start without it (an empty HS256 key makes
tokens forgeable). `users.IssueToken` in `token.go` is the only signer; the
token also goes out in the `access_token` HttpOnly cookie for browsers.

`Auth` reads the token from `Authorization: Bearer <token>` (priority) or
that cookie, verifies it with `users.ParseToken`, loads the user and puts it
in the request context; handlers read it with `users.FromContext(ctx)`.
`ParseToken` pins HS256 (`jwt.WithValidMethods` — otherwise `alg: none`
would authenticate anyone) and requires `exp`.

Revoking a stateless token is what `User.TokenVersion` (`token_version`,
absent = 0) is for: every token carries `ver`, `Auth` rejects a mismatch,
and a password change `$inc`s it in the same write as the new hash. Do NOT
replace this with comparing `iat` against a revocation timestamp — `iat` is
truncated to whole seconds, so a token issued in the same second as the
change is judged wrongly (TestUpdateUserRoute caught exactly that). Only
`sub` and `ver` are trusted: role and email always come from storage, so a
token minted before a demotion or a soft delete carries no stale powers.

Collections and their invariants (created in `users.NewStorage`):
`users` — unique index on `email`;
`chat_credentials` — XMPP credentials of the mirrored account, `_id` = user id;
`chats` — participants are validated against `users` at creation time and the
creator from the request context is always included.

Startup: `slog.SetDefault` in `main` configures the text logger all packages
use. Server shutdown is graceful (signal.NotifyContext + srv.Shutdown).

## Notes

- Windows dev environment: Git Bash `kill` cannot deliver Ctrl+C to the native
  server binary (it hard-kills). To exercise graceful shutdown, send
  CTRL_BREAK_EVENT via WinAPI to a process started with CREATE_NEW_PROCESS_GROUP.
