import axios, { AxiosError } from 'axios'

// One axios instance for the whole app. baseURL is empty in development so
// requests stay same-origin and hit the Vite dev proxy (see vite.config.ts).
const client = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '',
})

const TOKEN_KEY = 'gocha_token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string | null) {
  if (token) {
    localStorage.setItem(TOKEN_KEY, token)
  } else {
    localStorage.removeItem(TOKEN_KEY)
  }
}

// Attach the bearer token to every request. The server also accepts the
// HttpOnly access_token cookie, but the token in localStorage is what lets
// the SPA survive a full reload, so we send it explicitly.
client.interceptors.request.use((config) => {
  const token = getToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// A 401 means the token is gone/expired/revoked: drop it and let the router
// guard bounce the user to the login page. onUnauthorized is wired up by the
// auth store so this module stays free of store/router imports (no cycles).
let onUnauthorized: (() => void) | null = null

export function setUnauthorizedHandler(handler: () => void) {
  onUnauthorized = handler
}

client.interceptors.response.use(
  (response) => response,
  (error: AxiosError) => {
    if (error.response?.status === 401) {
      setToken(null)
      onUnauthorized?.()
    }
    return Promise.reject(error)
  },
)

/** Pull the server's `{"error": "..."}` message out of an axios error, with a
 *  sensible fallback so callers can always show something useful. */
export function errorMessage(error: unknown, fallback = 'Something went wrong'): string {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data as { error?: string } | undefined
    if (data?.error) return data.error
    if (error.message) return error.message
  }
  return fallback
}

/** A 422 from the server carries a `fields` map (see internal/validate) —
 *  field name as the API spells it, e.g. `display_name`, to a message. Any
 *  other failure yields an empty map, so callers can always fall back to
 *  errorMessage() for the summary. */
export function fieldErrors(error: unknown): Record<string, string> {
  if (!axios.isAxiosError(error)) return {}
  const data = error.response?.data as { fields?: Record<string, string> } | undefined
  return data?.fields ?? {}
}

export default client
