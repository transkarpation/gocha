package users

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/transkarpation/gocha/internal/permissions"
)

// ruleParam digs the parameter of one validate rule out of a struct tag,
// e.g. ruleParam(t, registerRequest{}, "Password", "min") == "8".
func ruleParam(t *testing.T, payload any, field, rule string) string {
	t.Helper()
	sf, ok := reflect.TypeOf(payload).FieldByName(field)
	if !ok {
		t.Fatalf("%T has no field %s", payload, field)
	}
	for _, r := range strings.Split(sf.Tag.Get("validate"), ",") {
		name, param, found := strings.Cut(r, "=")
		if name == rule {
			if !found {
				t.Fatalf("rule %q on %T.%s has no parameter", rule, payload, field)
			}
			return param
		}
	}
	t.Fatalf("no %q rule on %T.%s (tag: %q)", rule, payload, field, sf.Tag.Get("validate"))
	return ""
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}

// The validator tags at the HTTP boundary duplicate limits that the service
// layer enforces for the CLI too. Struct tags cannot reference Go constants,
// so nothing but this test stops the two from drifting apart — and a drift
// means the API and gochactrl quietly disagree on what is valid.
func TestTagsMatchServiceLimits(t *testing.T) {
	t.Run("register password min", func(t *testing.T) {
		if got := mustAtoi(t, ruleParam(t, registerRequest{}, "Password", "min")); got != minPasswordLen {
			t.Errorf("tag min=%d, minPasswordLen=%d", got, minPasswordLen)
		}
	})

	t.Run("update password min", func(t *testing.T) {
		if got := mustAtoi(t, ruleParam(t, updateUserRequest{}, "Password", "min")); got != minPasswordLen {
			t.Errorf("tag min=%d, minPasswordLen=%d", got, minPasswordLen)
		}
	})

	t.Run("register display name max", func(t *testing.T) {
		if got := mustAtoi(t, ruleParam(t, registerRequest{}, "DisplayName", "max")); got != maxDisplayNameLen {
			t.Errorf("tag max=%d, maxDisplayNameLen=%d", got, maxDisplayNameLen)
		}
	})

	t.Run("update display name max", func(t *testing.T) {
		if got := mustAtoi(t, ruleParam(t, updateUserRequest{}, "DisplayName", "max")); got != maxDisplayNameLen {
			t.Errorf("tag max=%d, maxDisplayNameLen=%d", got, maxDisplayNameLen)
		}
	})

	t.Run("update role oneof lists every role", func(t *testing.T) {
		got := strings.Fields(ruleParam(t, updateUserRequest{}, "Role", "oneof"))
		want := make([]string, 0, len(permissions.Roles()))
		for _, r := range permissions.Roles() {
			want = append(want, string(r))
		}
		if len(got) != len(want) {
			t.Fatalf("oneof = %v, want the %d registered roles %v", got, len(want), want)
		}
		for _, role := range want {
			if !slices.Contains(got, role) {
				t.Errorf("oneof = %v, missing role %q", got, role)
			}
		}
	})
}

// Login must not constrain the password: a short one belongs to the
// credential check (401), not to input validation (422), which would tell
// an attacker that the password policy rejected it before it was checked.
func TestLoginRequestDoesNotConstrainPassword(t *testing.T) {
	sf, ok := reflect.TypeOf(loginRequest{}).FieldByName("Password")
	if !ok {
		t.Fatal("loginRequest has no Password field")
	}
	tag := sf.Tag.Get("validate")
	for _, rule := range strings.Split(tag, ",") {
		name, _, _ := strings.Cut(rule, "=")
		if name != "required" {
			t.Errorf("loginRequest.Password has rule %q (tag %q); only `required` is allowed", name, tag)
		}
	}
}

// The validator runs before the handler touches storage, so these cases
// exercise the real HTTP path with a nil Storage — no Mongo needed. If a
// request ever reached the service layer here, it would panic, which is
// exactly the regression worth catching: input must be rejected at the
// boundary, not deep inside.
func TestRegisterRejectsInvalidInput(t *testing.T) {
	h := NewHandler(nil, nil, []byte("test-secret"))

	tests := []struct {
		name      string
		body      string
		wantField string
	}{
		{"missing email", `{"password":"secret123"}`, "email"},
		{"malformed email", `{"email":"not-an-email","password":"secret123"}`, "email"},
		{"missing password", `{"email":"a@b.com"}`, "password"},
		{"short password", `{"email":"a@b.com","password":"1234567"}`, "password"},
		{
			"display name too long",
			`{"email":"a@b.com","password":"secret123","display_name":"` +
				strings.Repeat("a", maxDisplayNameLen+1) + `"}`,
			"display_name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			h.Register(w, req)

			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d (body: %s)", w.Code, http.StatusUnprocessableEntity, w.Body)
			}
			var body struct {
				Error  string            `json:"error"`
				Fields map[string]string `json:"fields"`
			}
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error == "" {
				t.Error("`error` must stay populated — the SPA renders that key")
			}
			if _, ok := body.Fields[tt.wantField]; !ok {
				t.Errorf("fields = %v, want an entry for %q", body.Fields, tt.wantField)
			}
		})
	}
}

// A valid-looking payload must get PAST validation — otherwise the tests
// above would pass even if the rules rejected everything. With a nil
// Storage the handler panics once it reaches the service layer, and that
// panic is the proof it got through.
func TestRegisterAcceptsValidInput(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("valid payload was rejected before reaching the service layer")
		}
	}()

	h := NewHandler(nil, nil, []byte("test-secret"))
	req := httptest.NewRequest(http.MethodPost, "/register",
		strings.NewReader(`{"email":"a@b.com","password":"secret123","display_name":"Alice"}`))
	h.Register(httptest.NewRecorder(), req)
}
