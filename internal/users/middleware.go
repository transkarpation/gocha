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

// Auth verifies the access token from the "Authorization: Bearer <token>"
// header or the access_token cookie and puts the user into the request
// context. Anything else gets 401.
func (h *Handler) Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := accessTokenFromRequest(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		u, err := h.authenticate(r.Context(), token)
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrInvalidToken):
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		case err != nil:
			slog.ErrorContext(r.Context(), "authenticate request", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey{}, u)))
	})
}

// authenticate resolves the user behind an access token. The signature is
// only half the check: the user is always loaded from storage, so a
// soft-deleted user is rejected (UserByID filters them out) and a token
// whose version is behind the user's — issued before the last password
// change — is refused. That version check is what makes a stateless token
// revocable.
func (h *Handler) authenticate(ctx context.Context, token string) (User, error) {
	claims, err := ParseToken(token, h.jwtSecret)
	if err != nil {
		return User{}, err
	}
	u, err := h.storage.UserByID(ctx, claims.UserID)
	if err != nil {
		return User{}, err
	}
	if claims.Version != u.TokenVersion {
		return User{}, ErrInvalidToken
	}
	return u, nil
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

// accessTokenFromRequest takes the Bearer header first so a client can
// override a stale cookie.
func accessTokenFromRequest(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if c, err := r.Cookie(accessTokenCookieName); err == nil {
		return c.Value
	}
	return ""
}
