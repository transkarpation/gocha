# gocha web

Single-page front end for the gocha server. Vue 3 + Vite + Pinia + vue-router,
talking to the Go API (`../main.go`) over axios.

## Setup

```sh
npm install
npm run dev      # dev server on http://localhost:5173
npm run build    # type-check + production build into dist/
npm run preview  # serve the production build locally
```

The dev server proxies the API paths (`/register`, `/login`, `/me`, `/users`,
`/chats`, `/healthcheck`) to the Go server so the browser makes only
same-origin requests — the server sends no CORS headers, so a direct
cross-origin call from `:5173` to `:8080` would be blocked. Point the proxy
elsewhere with `API_PROXY_TARGET` (defaults to `http://localhost:8080`):

```sh
API_PROXY_TARGET=http://localhost:9000 npm run dev
```

Some API paths (`/login`, `/register`, `/chats`) double as client-side router
URLs. The proxy bypasses HTML navigations (serving the SPA) and forwards only
XHR API calls — see `vite.config.ts`.

Run the backend first (from the repo root): `task up` then `task run`. Create
an admin to exercise the Users screen:

```sh
./bin/gochactrl.exe register --email admin@example.com --password admin12345 --role admin
```

## How it maps to the server

| Screen        | Routes used                                                        |
| ------------- | ------------------------------------------------------------------ |
| Login         | `POST /login`, then `GET /me` for the role                         |
| Register      | `POST /register`, then `GET /me`                                   |
| Home          | shows `GET /me` and the XMPP credentials from the sign-in payload  |
| Chats         | `POST /chats`, `DELETE /chats/{id}` (admin)                        |
| Chat          | `GET /chats/{id}/messages`, `POST /chats/{id}/messages`            |
| Users (admin) | `GET /users`, `PATCH /users/{id}`, `DELETE`, `POST .../restore`    |

### Auth

The token from `/login` / `/register` is stored in `localStorage` and sent as
`Authorization: Bearer <token>` by an axios interceptor (`src/api/client.ts`).
The sign-in payload carries no `role`, so the app calls `GET /me` right after
to learn it; the router guard resolves `/me` once on reload before evaluating
admin-only routes. A `401` clears the token and bounces to `/login`.

### The chat list is client-side

The API has **no endpoint to list chats** — only create, delete, send, and
read messages. So `src/stores/chats.ts` keeps a `localStorage` registry of
chats you created or opened by ID. It's a convenience, not a source of truth:
a chat can exist on the server without appearing here.

## Layout

```
src/
  api/client.ts        axios instance, token storage, 401 handling
  services/            thin wrappers, one per API area (auth, users, chats)
  stores/              Pinia: auth (token/user/xmpp), chats (local registry)
  views/               one component per route
  router/index.ts      routes + auth/guest/admin guards
  types.ts             API response shapes
```
