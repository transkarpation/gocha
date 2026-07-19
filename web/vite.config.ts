import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'
import vueDevTools from 'vite-plugin-vue-devtools'
import tailwindcss from '@tailwindcss/vite'

// Where the Go server listens. Override with API_PROXY_TARGET if it runs
// somewhere else (e.g. a different port from config.yaml / APP_PORT).
const apiTarget = process.env.API_PROXY_TARGET ?? 'http://localhost:8080'

// The Go server exposes these top-level paths. With VITE_API_URL set (see
// .env.development) the SPA calls the server directly cross-origin — the
// backend now sends CORS headers (server.allowed_origins), so this proxy is
// only a fallback for when VITE_API_URL is empty and requests stay
// same-origin. Keep this list in sync with setupRouter in main.go.
const apiPaths = ['/register', '/login', '/me', '/users', '/chats', '/healthcheck']

// Some of these paths (/login, /register, /chats) are also client-side router
// URLs. A browser navigation to them must load the SPA, not hit the API. Tell
// the proxy to bypass (serve index.html) for HTML navigations; real API calls
// come from axios as XHR and never send `Accept: text/html`, so they proxy
// through as normal.
function bypassHtmlNavigation(req: { method?: string; headers: Record<string, unknown> }) {
  const accept = String(req.headers.accept ?? '')
  if (req.method === 'GET' && accept.includes('text/html')) {
    return '/index.html'
  }
  return undefined
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    vueJsx(),
    vueDevTools(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: Object.fromEntries(
      apiPaths.map((path) => [
        path,
        { target: apiTarget, changeOrigin: true, bypass: bypassHtmlNavigation },
      ]),
    ),
  },
})
