package users

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/transkarpation/gocha/internal/permissions"
)

// authTestEnv registers a user, issues a session and returns a handler
// that reports whether the request reached it with the user in context.
func authTestEnv(t *testing.T) (*Storage, *Handler) {
	t.Helper()
	s := newTestStorage(t)
	return s, NewHandler(s)
}

func echoUser(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := FromContext(r.Context())
		if !ok {
			t.Error("user missing from context inside protected handler")
		}
		w.Write([]byte(u.Email))
	}
}

func TestAuthMiddleware(t *testing.T) {
	s, h := authTestEnv(t)
	ctx := context.Background()

	u, err := Register(ctx, s, "alice@example.com", "secret123", permissions.RoleUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	sess, err := IssueSession(ctx, s, u)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	protected := h.Auth(echoUser(t))

	tests := []struct {
		name       string
		setup      func(r *http.Request)
		wantStatus int
	}{
		{"no token", func(r *http.Request) {}, http.StatusUnauthorized},
		{"valid bearer", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+sess.Token)
		}, http.StatusOK},
		{"valid cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.Token})
		}, http.StatusOK},
		{"garbage token", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer deadbeef")
		}, http.StatusUnauthorized},
		{"bearer wins over bad cookie", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+sess.Token)
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "garbage"})
		}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tt.setup(req)
			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRequirePermission(t *testing.T) {
	s, h := authTestEnv(t)
	ctx := context.Background()

	admin, err := Register(ctx, s, "admin@example.com", "secret123", permissions.RoleAdmin)
	if err != nil {
		t.Fatalf("Register admin: %v", err)
	}
	user, err := Register(ctx, s, "user@example.com", "secret123", permissions.RoleUser)
	if err != nil {
		t.Fatalf("Register user: %v", err)
	}
	adminSess, err := IssueSession(ctx, s, admin)
	if err != nil {
		t.Fatalf("IssueSession admin: %v", err)
	}
	userSess, err := IssueSession(ctx, s, user)
	if err != nil {
		t.Fatalf("IssueSession user: %v", err)
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	protected := h.Auth(RequirePermission(permissions.ChatsDelete)(ok))

	tests := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{"admin allowed", adminSess.Token, http.StatusOK},
		{"user forbidden", userSess.Token, http.StatusForbidden},
		{"anonymous unauthorized", "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/", nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}

	t.Run("without auth middleware user is missing", func(t *testing.T) {
		bare := RequirePermission(permissions.ChatsDelete)(ok)
		req := httptest.NewRequest(http.MethodDelete, "/", nil)
		rec := httptest.NewRecorder()
		bare.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}
