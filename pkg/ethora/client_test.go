package ethora

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	if claims["type"] != "server" {
		t.Errorf(`claim type = %v, want "server"`, claims["type"])
	}
	if claims["appId"] != testKey {
		t.Errorf("claim appId = %v, want %q", claims["appId"], testKey)
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
