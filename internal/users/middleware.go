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

// Auth authenticates the request with the credential from the "session"
// cookie or the "Authorization: Bearer <token>" header — either an opaque
// session token or a JWT access token — and puts the user into the request
// context. Anything else gets 401.
func (h *Handler) Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := authTokenFromRequest(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		u, err := h.authenticate(r.Context(), token)
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrInvalidToken):
			writeError(w, http.StatusUnauthorized, "invalid or expired credentials")
			return
		case err != nil:
			slog.ErrorContext(r.Context(), "authenticate request", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey{}, u)))
	})
}

// authenticate resolves the user behind a credential. Session tokens are
// hex, so only an access token has the header.payload.signature shape —
// that tells the two apart without a pointless database lookup.
//
// Either way the user is loaded from storage, so soft-deleted users are
// rejected (UserByID filters them out). Note the asymmetry: sessions are
// killed on password change, while an access token stays valid until it
// expires — it is stateless by design.
func (h *Handler) authenticate(ctx context.Context, token string) (User, error) {
	if strings.Count(token, ".") == 2 {
		id, err := ParseToken(token, h.jwtSecret)
		if err != nil {
			return User{}, err
		}
		return h.storage.UserByID(ctx, id)
	}

	sess, err := h.storage.SessionByToken(ctx, token)
	if err != nil {
		return User{}, err
	}
	return h.storage.UserByID(ctx, sess.UserID)
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

// authTokenFromRequest takes the Bearer header first so a client can
// override a stale cookie.
func authTokenFromRequest(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		return c.Value
	}
	return ""
}
