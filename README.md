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

## Configuration

Priority: **env vars > `config.yaml` > built-in defaults**.

| Setting | config.yaml | Env override | Default |
|---|---|---|---|
| HTTP port | `server.port` | `APP_PORT` | `8080` |
| Mongo URI | `mongo.uri` | `MONGO_URI` | local docker-compose instance |
| Mongo database | `mongo.database` | `MONGO_DB` | `protected_server` |

`config.yaml` is gitignored; copy it from `config.example.yaml`. A missing
file is fine — defaults are used. Both binaries accept `-config <path>`.

## API

Public:

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthcheck` | liveness probe |
| `POST` | `/register` | create account (`email`, `password`), returns a session |
| `POST` | `/login` | verify credentials, returns a session |

Authenticated (send `Authorization: Bearer <token>` or the `session` cookie):

| Method | Path | Permission | Description |
|---|---|---|---|
| `GET` | `/me` | — | current user |
| `GET` | `/users` | `users:read` (admin only) | list users, oldest first (`?limit=`, `?offset=`) |
| `DELETE` | `/users/{id}` | `users:delete` (admin only) | delete a user (and their Ethora mirror) |
| `POST` | `/chats` | `chats:create` | create a chat (`name`, `type`: `public`/`group`, `participants`: user ids) |
| `DELETE` | `/chats/{id}` | `chats:delete` (admin only) | delete a chat |
| `POST` | `/chats/{id}/messages` | `messages:create` | send a message (`text`) |
| `GET` | `/chats/{id}/messages` | `messages:read` | list messages, newest first (`?limit=`, `?offset=`) |

Group chats are participants-only; public chats are open to any authenticated
user. Sessions live 24h and are issued on register and login.

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
go build -o bin/gochactrl.exe ./cmd/gochactrl

./bin/gochactrl.exe register --email admin@example.com --password secret123 --role admin
./bin/gochactrl.exe login --email admin@example.com --password secret123

# delete a user bypassing permission checks (direct DB access)
./bin/gochactrl.exe delete --email someone@example.com   # or --id <hex>

# list users (tab-separated: id, role, created_at, email)
./bin/gochactrl.exe list --limit 20 --offset 0
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
