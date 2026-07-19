package main

import (
	"net/http"
)

// corsMiddleware adds CORS response headers so the SPA can call the API
// cross-origin (e.g. the Vite dev server on :5173 hitting the API on :8080)
// without a dev proxy.
//
// allowedOrigins is an explicit allowlist; a single "*" entry allows any
// origin but — per the CORS spec — without credentials. For a listed origin
// we reflect it and allow credentials so the access_token cookie works too.
// The token also travels in the Authorization header, which is why that
// header is always allowed. Preflight OPTIONS requests are answered here.
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAll := false
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowAll || allowed[origin]) {
				if allowAll {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					// Reflecting the specific origin is required to allow credentials.
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				w.Header().Add("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "300")
			}

			// Short-circuit the preflight; there are no other OPTIONS routes.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
