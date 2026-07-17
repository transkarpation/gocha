package users

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/transkarpation/gocha/internal/permissions"
)

// testJWTSecret signs access tokens in handler tests.
var testJWTSecret = []byte("test-jwt-secret")

// authTestEnv registers a user, issues a session and returns a handler
// that reports whether the request reached it with the user in context.
func authTestEnv(t *testing.T) (*Storage, *Handler) {
	t.Helper()
	s := newTestStorage(t)
	return s, NewHandler(s, nil, testJWTSecret)
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

// TestRegisterLoginResponse covers what a client gets when signing in:
// a session, a JWT access token signed with our secret, and the XMPP
// credentials of its mirrored chat account.
func TestRegisterLoginResponse(t *testing.T) {
	s := newTestStorage(t)
	chat := &fakeChat{}
	h := NewHandler(s, chat, testJWTSecret)

	body := strings.NewReader(`{"email":"alice@example.com","password":"secret123"}`)
	req := httptest.NewRequest(http.MethodPost, "/register", body)
	rec := httptest.NewRecorder()
	h.Register(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	userID, _ := got["id"].(string)
	if userID == "" {
		t.Fatalf("no user id in response: %v", got)
	}
	if got["session_token"] == "" {
		t.Error("no session_token in response")
	}
	if got["xmpp_username"] != "xmpp-"+userID || got["xmpp_password"] != "xmpp-pass-"+userID {
		t.Errorf("xmpp credentials = %v / %v", got["xmpp_username"], got["xmpp_password"])
	}

	// The access token must verify with our secret and describe the user.
	token, _ := got["access_token"].(string)
	if token == "" {
		t.Fatal("no access_token in response")
	}
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(tok *jwt.Token) (any, error) {
		if tok.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return testJWTSecret, nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("parse access token: %v", err)
	}
	if claims["sub"] != userID {
		t.Errorf("claim sub = %v, want %s", claims["sub"], userID)
	}
	if claims["email"] != "alice@example.com" {
		t.Errorf("claim email = %v", claims["email"])
	}
	if claims["role"] != string(permissions.RoleUser) {
		t.Errorf("claim role = %v, want user", claims["role"])
	}
	if _, err := claims.GetExpirationTime(); err != nil {
		t.Errorf("claim exp: %v", err)
	}
	// A token signed with another secret must not verify.
	if _, err := jwt.Parse(token, func(*jwt.Token) (any, error) {
		return []byte("wrong-secret"), nil
	}); err == nil {
		t.Error("access token verified with the wrong secret")
	}

	// Login hands out the same material.
	body = strings.NewReader(`{"email":"alice@example.com","password":"secret123"}`)
	req = httptest.NewRequest(http.MethodPost, "/login", body)
	rec = httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var loggedIn map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	for _, key := range []string{"access_token", "session_token", "xmpp_username", "xmpp_password"} {
		if v, _ := loggedIn[key].(string); v == "" {
			t.Errorf("login response missing %s", key)
		}
	}
}

// A user without a mirrored chat account must still be able to sign in.
func TestLoginWithoutChatCredentials(t *testing.T) {
	s := newTestStorage(t)
	h := NewHandler(s, nil, testJWTSecret)

	if _, err := Register(context.Background(), s, nil, "nomirror@example.com", "secret123", permissions.RoleUser); err != nil {
		t.Fatalf("Register: %v", err)
	}

	body := strings.NewReader(`{"email":"nomirror@example.com","password":"secret123"}`)
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if _, ok := got["xmpp_username"]; ok {
		t.Errorf("xmpp_username present without a mirror: %v", got)
	}
	if v, _ := got["access_token"].(string); v == "" {
		t.Error("no access_token in response")
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

func TestUpdateUserRoute(t *testing.T) {
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
		r.With(RequirePermission(permissions.UsersUpdate)).Patch("/users/{id}", h.Update)
	})

	do := func(path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	victimURL := "/users/" + victim.ID.Hex()

	if rec := do(victimURL, victimSess.Token, `{"role":"admin"}`); rec.Code != http.StatusForbidden {
		t.Errorf("plain user update: status = %d, want 403", rec.Code)
	}

	rec := do(victimURL, adminSess.Token, `{"role":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("role update: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["role"] != "admin" {
		t.Errorf("role = %v, want admin", resp["role"])
	}
	if u, _ := s.UserByID(ctx, victim.ID); u.Role != permissions.RoleAdmin {
		t.Errorf("stored role = %q, want admin", u.Role)
	}

	validation := []struct {
		name string
		path string
		body string
		want int
	}{
		{"empty body", victimURL, `{}`, http.StatusUnprocessableEntity},
		{"invalid email", victimURL, `{"email":"not-an-email"}`, http.StatusUnprocessableEntity},
		{"short password", victimURL, `{"password":"123"}`, http.StatusUnprocessableEntity},
		{"invalid role", victimURL, `{"role":"superuser"}`, http.StatusUnprocessableEntity},
		{"duplicate email", victimURL, `{"email":"admin@example.com"}`, http.StatusConflict},
		{"bad id", "/users/not-hex", `{"role":"user"}`, http.StatusUnprocessableEntity},
		{"missing user", "/users/000000000000000000000000", `{"role":"user"}`, http.StatusNotFound},
	}
	for _, tt := range validation {
		t.Run(tt.name, func(t *testing.T) {
			if rec := do(tt.path, adminSess.Token, tt.body); rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	// Password change invalidates the victim's sessions.
	if rec := do(victimURL, adminSess.Token, `{"password":"newsecret123"}`); rec.Code != http.StatusOK {
		t.Fatalf("password update: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := s.SessionByToken(ctx, victimSess.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("victim session survived password change: %v", err)
	}
	if _, err := Login(ctx, s, "victim@example.com", "newsecret123"); err != nil {
		t.Errorf("login with new password: %v", err)
	}
	if _, err := Login(ctx, s, "victim@example.com", "secret123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("login with old password: %v, want ErrInvalidCredentials", err)
	}
}

func TestRestoreUserRoute(t *testing.T) {
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
		r.With(RequirePermission(permissions.UsersUpdate)).Post("/users/{id}/restore", h.Restore)
	})

	do := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	restoreURL := "/users/" + victim.ID.Hex() + "/restore"

	if rec := do(restoreURL, victimSess.Token); rec.Code != http.StatusForbidden {
		t.Errorf("plain user restore: status = %d, want 403", rec.Code)
	}
	if rec := do(restoreURL, adminSess.Token); rec.Code != http.StatusConflict {
		t.Errorf("restore alive user: status = %d, want 409", rec.Code)
	}

	if err := DeleteUser(ctx, s, victim.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	rec := do(restoreURL, adminSess.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["email"] != "victim@example.com" {
		t.Errorf("restored user = %v", resp)
	}
	if rec := do("/users/not-hex/restore", adminSess.Token); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad id: status = %d, want 422", rec.Code)
	}
	if rec := do("/users/000000000000000000000000/restore", adminSess.Token); rec.Code != http.StatusNotFound {
		t.Errorf("missing user: status = %d, want 404", rec.Code)
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
