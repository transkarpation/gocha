package users

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/transkarpation/gocha/internal/permissions"
)

type userCtxKey struct{}

// FromContext returns the user stored by the Auth middleware.
func FromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userCtxKey{}).(User)
	return u, ok
}

// Auth verifies the session token from the "session" cookie or the
// "Authorization: Bearer <token>" header and puts the user into the
// request context. Requests without a valid session get 401.
func (h *Handler) Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := sessionTokenFromRequest(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		sess, err := h.storage.SessionByToken(r.Context(), token)
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "lookup session", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		u, err := h.storage.UserByID(r.Context(), sess.UserID)
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "lookup session user", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey{}, u)))
	})
}

// RequirePermission guards a route with a permission check.
// Must be applied after Auth, which puts the user into the context.
func RequirePermission(p permissions.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := FromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if !permissions.Has(u.Role, p) {
				writeError(w, http.StatusForbidden, "permission denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sessionTokenFromRequest(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		return c.Value
	}
	return ""
}
