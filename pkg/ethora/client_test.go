package ethora

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testKey    = "test-app-id"
	testSecret = "test-secret"
)

func TestAppToken(t *testing.T) {
	c := NewClient("https://example.com", testKey, testSecret)

	token, err := c.AppToken()
	if err != nil {
		t.Fatalf("AppToken: %v", err)
	}

	parsed, err := jwt.Parse(token, func(tok *jwt.Token) (any, error) {
		if tok.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(testSecret), nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	data, ok := claims["data"].(map[string]any)
	if !ok {
		t.Fatalf(`claims must be nested under "data", got %v`, claims)
	}
	if data["type"] != "server" {
		t.Errorf(`claim data.type = %v, want "server"`, data["type"])
	}
	if data["appId"] != testKey {
		t.Errorf("claim data.appId = %v, want %q", data["appId"], testKey)
	}

	// A token signed with a different secret must not verify.
	if _, err := jwt.Parse(token, func(*jwt.Token) (any, error) {
		return []byte("wrong-secret"), nil
	}); err == nil {
		t.Error("token verified with wrong secret")
	}
}

func TestCreateUser(t *testing.T) {
	var gotAuth, gotPath, gotMethod string
	var gotBody CreateUserRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{
				"_id":          "e1",
				"uuid":         gotBody.UUID,
				"appId":        testKey,
				"firstName":    gotBody.FirstName,
				"xmppPassword": "xmpp-pass",
			},
		})
	}))
	defer srv.Close()

	// Trailing slash must not produce a double slash in the request path.
	c := NewClient(srv.URL+"/", testKey, testSecret)
	u, err := c.CreateUser(context.Background(), CreateUserRequest{
		UUID:      "user-1",
		FirstName: "Alice",
		LastName:  "Example",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/v1/users" {
		t.Errorf("request = %s %s, want POST /v1/users", gotMethod, gotPath)
	}
	if gotBody.UUID != "user-1" || gotBody.FirstName != "Alice" {
		t.Errorf("request body = %+v", gotBody)
	}
	if u.ID != "e1" || u.UUID != "user-1" || u.XMPPPassword != "xmpp-pass" {
		t.Errorf("user = %+v", u)
	}

	// The Authorization header must carry a JWT verifiable with the secret.
	if _, err := jwt.Parse(gotAuth, func(*jwt.Token) (any, error) {
		return []byte(testSecret), nil
	}); err != nil {
		t.Errorf("Authorization header is not a valid token: %v", err)
	}
}

func TestCreateUsersBatch(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody struct {
		BypassEmailConfirmation bool        `json:"bypassEmailConfirmation"`
		UsersList               []BatchUser `json:"usersList"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)

		// v1 creates synchronously and returns the users in "results".
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "success": true,
			"results": []map[string]any{
				{"_id": "e1", "uuid": "id-1", "appId": testKey, "firstName": "A"},
				{"_id": "e2", "uuid": "id-2", "appId": testKey, "firstName": "C"},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testKey, testSecret)
	batch := []BatchUser{
		{Email: "a@b.com", FirstName: "A", LastName: "B", Password: "pw1", UUID: "id-1"},
		{Email: "c@d.com", FirstName: "C", LastName: "D", Password: "pw2", UUID: "id-2"},
	}
	created, err := c.CreateUsersBatch(context.Background(), batch, false)
	if err != nil {
		t.Fatalf("CreateUsersBatch: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/v1/users/batch" {
		t.Errorf("request = %s %s, want POST /v1/users/batch", gotMethod, gotPath)
	}
	if gotBody.BypassEmailConfirmation {
		t.Error("bypassEmailConfirmation = true, want false")
	}
	if len(gotBody.UsersList) != 2 || gotBody.UsersList[0].UUID != "id-1" || gotBody.UsersList[1].Email != "c@d.com" {
		t.Errorf("usersList = %+v", gotBody.UsersList)
	}
	if len(created) != 2 || created[0].ID != "e1" || created[1].UUID != "id-2" {
		t.Errorf("created = %+v", created)
	}
}

func TestRandomBatchUser(t *testing.T) {
	u1, err := RandomBatchUser("a@b.com", "id-1")
	if err != nil {
		t.Fatalf("RandomBatchUser: %v", err)
	}
	u2, err := RandomBatchUser("a@b.com", "id-1")
	if err != nil {
		t.Fatalf("RandomBatchUser: %v", err)
	}

	if u1.Email != "a@b.com" || u1.UUID != "id-1" {
		t.Errorf("email/uuid not preserved: %+v", u1)
	}
	if len(u1.Password) != 32 {
		t.Errorf("password length = %d, want 32", len(u1.Password))
	}
	if u1.Password == u2.Password {
		t.Error("passwords must be unique per call")
	}
	if u1.FirstName == "" || u1.LastName == "" {
		t.Errorf("names must be non-empty: %+v", u1)
	}
	if u1.FirstName == u2.FirstName && u1.LastName == u2.LastName {
		t.Error("names must be random per call")
	}
}

func TestDeleteUsersBatch(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	var gotBody struct {
		UsersIDList []string `json:"usersIdList"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testKey, testSecret)
	if err := c.DeleteUsersBatch(context.Background(), []string{"id-1", "id-2"}); err != nil {
		t.Fatalf("DeleteUsersBatch: %v", err)
	}

	if gotMethod != http.MethodDelete || gotPath != "/v1/users/batch" {
		t.Errorf("request = %s %s, want DELETE /v1/users/batch", gotMethod, gotPath)
	}
	if len(gotBody.UsersIDList) != 2 || gotBody.UsersIDList[0] != "id-1" || gotBody.UsersIDList[1] != "id-2" {
		t.Errorf("usersIdList = %v", gotBody.UsersIDList)
	}
	if _, err := jwt.Parse(gotAuth, func(*jwt.Token) (any, error) {
		return []byte(testSecret), nil
	}); err != nil {
		t.Errorf("Authorization header is not a valid token: %v", err)
	}
}

func TestRedactPasswords(t *testing.T) {
	body, err := json.Marshal(batchCreateRequest{
		BypassEmailConfirmation: true,
		UsersList: []BatchUser{
			{Email: "a@b.com", FirstName: "A", LastName: "B", Password: "super-secret", UUID: "id-1"},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := redactPasswords(body)
	if strings.Contains(got, "super-secret") {
		t.Errorf("password leaked into log output: %s", got)
	}
	if !strings.Contains(got, `"password":"[redacted]"`) {
		t.Errorf("password not masked: %s", got)
	}

	// Response bodies carry password-ish keys too (e.g. xmppPassword).
	resp := redactPasswords([]byte(`{"user":{"uuid":"id-1","xmppPassword":"xmpp-secret"}}`))
	if strings.Contains(resp, "xmpp-secret") || !strings.Contains(resp, `"xmppPassword":"[redacted]"`) {
		t.Errorf("xmppPassword not masked: %s", resp)
	}
	// Everything else must survive redaction.
	for _, want := range []string{"a@b.com", "id-1", `"bypassEmailConfirmation":true`} {
		if !strings.Contains(got, want) {
			t.Errorf("output lost %q: %s", want, got)
		}
	}

	if got := redactPasswords([]byte("not json")); got != "<non-json body>" {
		t.Errorf("non-JSON body = %q", got)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"bad token"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testKey, testSecret)
	_, err := c.CreateUser(context.Background(), CreateUserRequest{UUID: "x"})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", apiErr.StatusCode)
	}
}
