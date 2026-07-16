package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/transkarpation/gocha/internal/permissions"
)

// authTestEnv registers a user, issues a session and returns a handler
// that reports whether the request reached it with the user in context.
func authTestEnv(t *testing.T) (*Storage, *Handler) {
	t.Helper()
	s := newTestStorage(t)
	return s, NewHandler(s, nil)
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

	u, err := Register(ctx, s, nil, "alice@example.com", "secret123", permissions.RoleUser)
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

	admin, err := Register(ctx, s, nil, "admin@example.com", "secret123", permissions.RoleAdmin)
	if err != nil {
		t.Fatalf("Register admin: %v", err)
	}
	user, err := Register(ctx, s, nil, "user@example.com", "secret123", permissions.RoleUser)
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

func TestListUsersRoute(t *testing.T) {
	s, h := authTestEnv(t)
	ctx := context.Background()

	admin, err := Register(ctx, s, nil, "admin@example.com", "secret123", permissions.RoleAdmin)
	if err != nil {
		t.Fatalf("Register admin: %v", err)
	}
	user, err := Register(ctx, s, nil, "user@example.com", "secret123", permissions.RoleUser)
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

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(h.Auth)
		r.With(RequirePermission(permissions.UsersRead)).Get("/users", h.List)
	})

	do := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("/users", userSess.Token); rec.Code != http.StatusForbidden {
		t.Errorf("plain user list: status = %d, want 403", rec.Code)
	}
	if rec := do("/users?limit=0", adminSess.Token); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad limit: status = %d, want 422", rec.Code)
	}

	rec := do("/users", adminSess.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Users) != 2 {
		t.Fatalf("got %d users, want 2", len(resp.Users))
	}
	if resp.Users[0]["email"] != "admin@example.com" || resp.Users[0]["role"] != "admin" {
		t.Errorf("first user = %v, want admin (oldest first)", resp.Users[0])
	}
	for _, u := range resp.Users {
		for k := range u {
			if k == "password_hash" || k == "PasswordHash" {
				t.Errorf("password hash leaked in response: %v", u)
			}
		}
	}

	if rec := do("/users?limit=1&offset=1", adminSess.Token); rec.Code == http.StatusOK {
		var page struct {
			Users []map[string]any `json:"users"`
		}
		json.Unmarshal(rec.Body.Bytes(), &page)
		if len(page.Users) != 1 || page.Users[0]["email"] != "user@example.com" {
			t.Errorf("pagination result = %v, want [user@example.com]", page.Users)
		}
	} else {
		t.Errorf("paginated list: status = %d, want 200", rec.Code)
	}
}

func TestDeleteUserRoute(t *testing.T) {
	s, h := authTestEnv(t)
	ctx := context.Background()

	admin, err := Register(ctx, s, nil, "admin@example.com", "secret123", permissions.RoleAdmin)
	if err != nil {
		t.Fatalf("Register admin: %v", err)
	}
	victim, err := Register(ctx, s, nil, "victim@example.com", "secret123", permissions.RoleUser)
	if err != nil {
		t.Fatalf("Register victim: %v", err)
	}
	adminSess, err := IssueSession(ctx, s, admin)
	if err != nil {
		t.Fatalf("IssueSession admin: %v", err)
	}
	victimSess, err := IssueSession(ctx, s, victim)
	if err != nil {
		t.Fatalf("IssueSession victim: %v", err)
	}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(h.Auth)
		r.With(RequirePermission(permissions.UsersDelete)).Delete("/users/{id}", h.Delete)
	})

	do := func(path, token string) int {
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do("/users/"+victim.ID.Hex(), victimSess.Token); code != http.StatusForbidden {
		t.Errorf("plain user delete: status = %d, want 403", code)
	}
	if code := do("/users/not-hex", adminSess.Token); code != http.StatusUnprocessableEntity {
		t.Errorf("invalid id: status = %d, want 422", code)
	}
	if code := do("/users/"+victim.ID.Hex(), adminSess.Token); code != http.StatusNoContent {
		t.Errorf("admin delete: status = %d, want 204", code)
	}
	if code := do("/users/"+victim.ID.Hex(), adminSess.Token); code != http.StatusNotFound {
		t.Errorf("second delete: status = %d, want 404", code)
	}
	// The victim's session died with the account.
	if code := do("/users/"+admin.ID.Hex(), victimSess.Token); code != http.StatusUnauthorized {
		t.Errorf("deleted user's token: status = %d, want 401", code)
	}
}
