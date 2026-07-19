package chats

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/transkarpation/gocha/internal/permissions"
	"github.com/transkarpation/gocha/internal/testutil"
	"github.com/transkarpation/gocha/internal/users"
)

func TestParseParticipants(t *testing.T) {
	creator := bson.NewObjectID()
	other := bson.NewObjectID()

	t.Run("creator always included and deduped", func(t *testing.T) {
		got, err := parseParticipants([]string{creator.Hex(), other.Hex(), other.Hex()}, creator)
		if err != nil {
			t.Fatalf("parseParticipants: %v", err)
		}
		if len(got) != 2 || got[0] != creator || got[1] != other {
			t.Errorf("got %v, want [creator, other]", got)
		}
	})

	t.Run("empty list keeps creator", func(t *testing.T) {
		got, err := parseParticipants(nil, creator)
		if err != nil {
			t.Fatalf("parseParticipants: %v", err)
		}
		if len(got) != 1 || got[0] != creator {
			t.Errorf("got %v, want [creator]", got)
		}
	})

	t.Run("invalid hex rejected", func(t *testing.T) {
		if _, err := parseParticipants([]string{"not-hex"}, creator); err == nil {
			t.Error("expected error for invalid participant id")
		}
	})
}

func TestIsParticipant(t *testing.T) {
	a, b := bson.NewObjectID(), bson.NewObjectID()
	c := Chat{Participants: []bson.ObjectID{a}}
	if !isParticipant(c, a) {
		t.Error("a must be a participant")
	}
	if isParticipant(c, b) {
		t.Error("b must not be a participant")
	}
}

// testJWTSecret signs the access tokens these tests authenticate with.
var testJWTSecret = []byte("test-jwt-secret")

// testEnv wires storages and the router exactly like main.go does and
// returns ready-to-use bearer tokens for two plain users and an admin.
type testEnv struct {
	router      *chi.Mux
	storage     *Storage
	userToken   string
	userID      bson.ObjectID
	otherToken  string
	otherID     bson.ObjectID
	adminToken  string
	strangerTok string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	db := testutil.MongoDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ustorage, err := users.NewStorage(ctx, db)
	if err != nil {
		t.Fatalf("users.NewStorage: %v", err)
	}
	cstorage, err := NewStorage(ctx, db)
	if err != nil {
		t.Fatalf("chats.NewStorage: %v", err)
	}

	uh := users.NewHandler(ustorage, nil, testJWTSecret)
	ch := NewHandler(cstorage, ustorage)

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(uh.Auth)
		r.With(users.RequirePermission(permissions.ChatsCreate)).Post("/chats", ch.Create)
		r.With(users.RequirePermission(permissions.ChatsDelete)).Delete("/chats/{id}", ch.Delete)
		r.With(users.RequirePermission(permissions.MessagesCreate)).Post("/chats/{id}/messages", ch.SendMessage)
		r.With(users.RequirePermission(permissions.MessagesRead)).Get("/chats/{id}/messages", ch.ListMessages)
	})

	env := &testEnv{router: r, storage: cstorage}
	register := func(email string, role permissions.Role) (bson.ObjectID, string) {
		u, err := users.Register(ctx, ustorage, nil, users.RegisterParams{Email: email, Password: "secret123", Role: role})
		if err != nil {
			t.Fatalf("register %s: %v", email, err)
		}
		token, err := users.IssueToken(u, testJWTSecret, users.TokenTTL)
		if err != nil {
			t.Fatalf("issue token %s: %v", email, err)
		}
		return u.ID, token
	}
	env.userID, env.userToken = register("user@example.com", permissions.RoleUser)
	env.otherID, env.otherToken = register("other@example.com", permissions.RoleUser)
	_, env.adminToken = register("admin@example.com", permissions.RoleAdmin)
	_, env.strangerTok = register("stranger@example.com", permissions.RoleUser)
	return env
}

func (e *testEnv) do(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return m
}

func TestCreateChat(t *testing.T) {
	env := newTestEnv(t)

	t.Run("group chat with participant", func(t *testing.T) {
		body := fmt.Sprintf(`{"name":"Team","type":"group","participants":["%s"]}`, env.otherID.Hex())
		rec := env.do(t, http.MethodPost, "/chats", env.userToken, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		resp := decodeJSON(t, rec)
		parts := resp["participants"].([]any)
		if len(parts) != 2 {
			t.Errorf("participants = %v, want creator + other", parts)
		}
		if resp["created_by"] != env.userID.Hex() {
			t.Errorf("created_by = %v, want %s", resp["created_by"], env.userID.Hex())
		}
	})

	t.Run("public chat without participants", func(t *testing.T) {
		rec := env.do(t, http.MethodPost, "/chats", env.userToken, `{"name":"Square","type":"public"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	validation := []struct {
		name string
		body string
	}{
		{"empty name", `{"name":"  ","type":"public"}`},
		{"long name", fmt.Sprintf(`{"name":"%s","type":"public"}`, strings.Repeat("x", 101))},
		{"bad type", `{"name":"X","type":"secret"}`},
		{"group without others", `{"name":"X","type":"group"}`},
		{"invalid participant id", `{"name":"X","type":"group","participants":["nope"]}`},
		{"nonexistent participant", `{"name":"X","type":"group","participants":["000000000000000000000000"]}`},
	}
	for _, tt := range validation {
		t.Run(tt.name, func(t *testing.T) {
			rec := env.do(t, http.MethodPost, "/chats", env.userToken, tt.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422 (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}

	t.Run("anonymous rejected", func(t *testing.T) {
		rec := env.do(t, http.MethodPost, "/chats", "", `{"name":"X","type":"public"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

func TestDeleteChat(t *testing.T) {
	env := newTestEnv(t)

	rec := env.do(t, http.MethodPost, "/chats", env.userToken, `{"name":"Doomed","type":"public"}`)
	chatID := decodeJSON(t, rec)["id"].(string)

	t.Run("user forbidden", func(t *testing.T) {
		rec := env.do(t, http.MethodDelete, "/chats/"+chatID, env.userToken, "")
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
	})
	t.Run("admin deletes", func(t *testing.T) {
		rec := env.do(t, http.MethodDelete, "/chats/"+chatID, env.adminToken, "")
		if rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204 (body: %s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("second delete is 404", func(t *testing.T) {
		rec := env.do(t, http.MethodDelete, "/chats/"+chatID, env.adminToken, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})
	t.Run("invalid id is 422", func(t *testing.T) {
		rec := env.do(t, http.MethodDelete, "/chats/not-hex", env.adminToken, "")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", rec.Code)
		}
	})
}

func TestMessages(t *testing.T) {
	env := newTestEnv(t)

	body := fmt.Sprintf(`{"name":"Team","type":"group","participants":["%s"]}`, env.otherID.Hex())
	groupID := decodeJSON(t, env.do(t, http.MethodPost, "/chats", env.userToken, body))["id"].(string)
	publicID := decodeJSON(t, env.do(t, http.MethodPost, "/chats", env.userToken, `{"name":"Square","type":"public"}`))["id"].(string)

	msgURL := func(chatID string) string { return "/chats/" + chatID + "/messages" }

	t.Run("participant sends and lists newest first", func(t *testing.T) {
		for i := 1; i <= 3; i++ {
			rec := env.do(t, http.MethodPost, msgURL(groupID), env.userToken, fmt.Sprintf(`{"text":"msg %d"}`, i))
			if rec.Code != http.StatusCreated {
				t.Fatalf("send %d: status = %d, body = %s", i, rec.Code, rec.Body.String())
			}
		}

		rec := env.do(t, http.MethodGet, msgURL(groupID), env.otherToken, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("list: status = %d, body = %s", rec.Code, rec.Body.String())
		}
		msgs := decodeJSON(t, rec)["messages"].([]any)
		if len(msgs) != 3 {
			t.Fatalf("got %d messages, want 3", len(msgs))
		}
		first := msgs[0].(map[string]any)
		if first["text"] != "msg 3" {
			t.Errorf("first message = %v, want newest (msg 3)", first["text"])
		}
		if first["author_id"] != env.userID.Hex() {
			t.Errorf("author_id = %v, want %s", first["author_id"], env.userID.Hex())
		}
	})

	t.Run("limit and offset", func(t *testing.T) {
		rec := env.do(t, http.MethodGet, msgURL(groupID)+"?limit=1&offset=2", env.userToken, "")
		msgs := decodeJSON(t, rec)["messages"].([]any)
		if len(msgs) != 1 || msgs[0].(map[string]any)["text"] != "msg 1" {
			t.Errorf("limit/offset result = %v, want [msg 1]", msgs)
		}
	})

	t.Run("invalid limit is 422", func(t *testing.T) {
		rec := env.do(t, http.MethodGet, msgURL(groupID)+"?limit=1000", env.userToken, "")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", rec.Code)
		}
	})

	t.Run("stranger cannot send or read group chat", func(t *testing.T) {
		if rec := env.do(t, http.MethodPost, msgURL(groupID), env.strangerTok, `{"text":"hi"}`); rec.Code != http.StatusForbidden {
			t.Errorf("send: status = %d, want 403", rec.Code)
		}
		if rec := env.do(t, http.MethodGet, msgURL(groupID), env.strangerTok, ""); rec.Code != http.StatusForbidden {
			t.Errorf("read: status = %d, want 403", rec.Code)
		}
	})

	t.Run("public chat open to any authenticated user", func(t *testing.T) {
		if rec := env.do(t, http.MethodPost, msgURL(publicID), env.strangerTok, `{"text":"hello"}`); rec.Code != http.StatusCreated {
			t.Errorf("send: status = %d, want 201", rec.Code)
		}
		if rec := env.do(t, http.MethodGet, msgURL(publicID), env.strangerTok, ""); rec.Code != http.StatusOK {
			t.Errorf("read: status = %d, want 200", rec.Code)
		}
	})

	t.Run("empty text is 422", func(t *testing.T) {
		rec := env.do(t, http.MethodPost, msgURL(groupID), env.userToken, `{"text":"   "}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", rec.Code)
		}
	})

	t.Run("nonexistent chat is 404", func(t *testing.T) {
		rec := env.do(t, http.MethodPost, msgURL("000000000000000000000000"), env.userToken, `{"text":"hi"}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("anonymous is 401", func(t *testing.T) {
		rec := env.do(t, http.MethodGet, msgURL(groupID), "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}
