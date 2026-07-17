# gocha

A small chat server in Go: session-based authentication, admin/user roles with
a central permission registry, chats and messages stored in MongoDB.

Built with [chi](https://github.com/go-chi/chi), the official
[MongoDB Go driver v2](https://github.com/mongodb/mongo-go-driver), bcrypt
password hashing and structured logging via `log/slog`.

## Quick start

```sh
# 1. Start MongoDB
docker compose up -d

# 2. (Optional) create a local config — defaults already match docker-compose
cp config.example.yaml config.yaml

# 3. Run the server
go run .
```

The server listens on `:8080` by default.

With [Task](https://taskfile.dev) installed
(`go install github.com/go-task/task/v3/cmd/task@latest`) the common
workflows are one word each:

```sh
task up      # start MongoDB
task build   # build bin/gocha.exe and bin/gochactrl.exe (incremental)
task run     # run the server
task test    # run all tests
task clean   # remove bin/
```

## Configuration

Priority: **env vars > `config.yaml` > built-in defaults**.

| Setting | config.yaml | Env override | Default |
|---|---|---|---|
| HTTP port | `server.port` | `APP_PORT` | `8080` |
| Mongo URI | `mongo.uri` | `MONGO_URI` | local docker-compose instance |
| Mongo database | `mongo.database` | `MONGO_DB` | `protected_server` |
| Ethora API | `ethora.base_url` | `ETHORA_BASE_URL` | `https://api.chat.ethora.com/` |
| Ethora credentials | `ethora.api_key` / `ethora.api_secret` | `ETHORA_API_KEY` / `ETHORA_API_SECRET` | unset (mirroring disabled) |
| XMPP websocket | `ethora.xmpp_ws_url` | `ETHORA_XMPP_WS_URL` | `wss://xmpp.chat.ethora.com/ws` |
| Access token key | `auth.jwt_secret` | `JWT_SECRET` | none — **required**, the server won't start without it (`openssl rand -hex 32`) |

`config.yaml` is gitignored; copy it from `config.example.yaml`. A missing
file is fine — defaults are used. Both binaries accept `-config <path>`.

## API

Public:

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthcheck` | liveness probe |
| `POST` | `/register` | create account (`email`, `password`), returns the sign-in payload |
| `POST` | `/login` | verify credentials, returns the sign-in payload |

Both return the same payload:

```json
{
  "id": "6a59d151811e1b8ac4bdd3fa",
  "email": "a@b.com",
  "session_token": "f31d1bea…",
  "access_token": "eyJhbGciOiJIUzI1NiIs…",
  "expires_at": "2026-07-18T06:53:05Z",
  "xmpp_username": "6a58fbd8…_6a59d151…",
  "xmpp_password": "dtmQJmIo7N"
}
```

`access_token` is a JWT (HS256, `auth.jwt_secret`) with `sub`, `email`,
`role`, `iat` and `exp` claims. `session_token` is what the API itself
authenticates with (see below). The `xmpp_*` fields are the credentials of
the user's mirrored chat account — connect to `ethora.xmpp_ws_url` with
them; they are absent when the user has no mirror.

Authenticated (send `Authorization: Bearer <token>` or the `session` cookie):

| Method | Path | Permission | Description |
|---|---|---|---|
| `GET` | `/me` | — | current user |
| `GET` | `/users` | `users:read` (admin only) | list users, oldest first (`?limit=`, `?offset=`) |
| `PATCH` | `/users/{id}` | `users:update` (admin only) | partial update (`email`, `role`, `password`); password change logs the user out |
| `DELETE` | `/users/{id}` | `users:delete` (admin only) | soft-delete a user (sets `deleted_at`, kills sessions; Ethora mirror is kept) |
| `POST` | `/users/{id}/restore` | `users:update` (admin only) | restore a soft-deleted user (409 if not deleted) |
| `POST` | `/chats` | `chats:create` | create a chat (`name`, `type`: `public`/`group`, `participants`: user ids) |
| `DELETE` | `/chats/{id}` | `chats:delete` (admin only) | delete a chat |
| `POST` | `/chats/{id}/messages` | `messages:create` | send a message (`text`) |
| `GET` | `/chats/{id}/messages` | `messages:read` | list messages, newest first (`?limit=`, `?offset=`) |

Group chats are participants-only; public chats are open to any authenticated
user. Sessions live 24h and are issued on register and login.

On startup the server ensures a `system@gocha.internal` account exists —
service messages are sent on its behalf. Its password is random and thrown
away, so it cannot be logged into; if soft-deleted, the next server start
restores it. The system account then connects to the Ethora XMPP server
over websocket (`ethora.xmpp_ws_url`) in a background goroutine and stays
connected, reconnecting automatically.

### Example

```sh
TOKEN=$(curl -s -X POST localhost:8080/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"a@b.com","password":"secret123"}' | jq -r .session_token)

curl -s -X POST localhost:8080/chats \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Town square","type":"public"}'
```

## Roles

HTTP self-registration always creates a plain `user`. Admins are created with
the CLI:

```sh
go build -o bin/gochactrl.exe ./cmd/gochactrl   # or: task build

./bin/gochactrl.exe register --email admin@example.com --password secret123 --role admin
./bin/gochactrl.exe login --email admin@example.com --password secret123

# soft-delete a user bypassing permission checks (direct DB access);
# --hard removes permanently, including the Ethora mirror
./bin/gochactrl.exe delete --email someone@example.com   # or --id <hex>

# restore a soft-deleted user
./bin/gochactrl.exe restore --email someone@example.com  # or --id <hex>

# list users (tab-separated: id, role, created_at, email)
./bin/gochactrl.exe list --limit 20 --offset 0

# wipe ALL users, sessions and their Ethora mirrors (destructive!);
# the system account is the one thing it never deletes
./bin/gochactrl.exe delete-all --yes

# show the system account (created at server startup) and its XMPP credentials
./bin/gochactrl.exe system

# create the system account without starting the server; errors if it exists
./bin/gochactrl.exe init-system
```

Role capabilities are defined in `internal/permissions` — the single registry
every HTTP entity adds its permissions to.

## Tests

```sh
go test ./...
```

`users` and `chats` tests are integration tests: they need the docker-compose
Mongo and skip themselves when it is not reachable. Each test runs in its own
throwaway database that is dropped afterwards.
