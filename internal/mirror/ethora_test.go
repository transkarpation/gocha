package mirror

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/transkarpation/gocha/pkg/ethora"
)

// TestDeleteUsersBatchFallback simulates Ethora's all-or-nothing batch
// delete: the batch call 404s because one id is unknown, then per-id
// deletes succeed (a 404 for the unknown id counts as success).
func TestDeleteUsersBatchFallback(t *testing.T) {
	var calls [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			UsersIDList []string `json:"usersIdList"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, body.UsersIDList)

		// Any request that includes the unknown id fails entirely.
		for _, id := range body.UsersIDList {
			if id == "unknown-id" {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"success":false,"code":"USER_NOT_FOUND"}`))
				return
			}
		}
		w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	e := NewEthora(ethora.NewClient(srv.URL, "key", "secret"))
	err := e.DeleteUsers(context.Background(), []string{"known-1", "unknown-id", "known-2"})
	if err != nil {
		t.Fatalf("DeleteUsers: %v", err)
	}

	// 1 batch attempt + 3 per-id fallback calls.
	if len(calls) != 4 {
		t.Fatalf("got %d calls, want 4: %v", len(calls), calls)
	}
	if len(calls[0]) != 3 {
		t.Errorf("first call = %v, want the full batch", calls[0])
	}
	for i, want := range []string{"known-1", "unknown-id", "known-2"} {
		if len(calls[i+1]) != 1 || calls[i+1][0] != want {
			t.Errorf("fallback call %d = %v, want [%s]", i+1, calls[i+1], want)
		}
	}
}

func TestDeleteUserNotFoundIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"success":false,"code":"USER_NOT_FOUND"}`))
	}))
	defer srv.Close()

	e := NewEthora(ethora.NewClient(srv.URL, "key", "secret"))
	if err := e.DeleteUser(context.Background(), "ghost"); err != nil {
		t.Errorf("DeleteUser on missing account: %v, want nil", err)
	}
}

func TestDeleteUsersRealFailureIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success":false}`))
	}))
	defer srv.Close()

	e := NewEthora(ethora.NewClient(srv.URL, "key", "secret"))
	if err := e.DeleteUsers(context.Background(), []string{"a", "b"}); err == nil {
		t.Error("expected error when every delete fails with 500")
	}
}

// Ethora requires both firstName and lastName, so the mapping from our
// single free-form display name onto that pair is what needs pinning down —
// especially the single-word and empty cases.
func TestSplitDisplayName(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantFirst string
		wantLast  string
	}{
		{"two words", "Alice Liddell", "Alice", "Liddell"},
		{"single word repeats", "Alice", "Alice", "Alice"},
		{"three words keep the tail together", "Ada King Lovelace", "Ada", "King Lovelace"},
		{"extra whitespace", "  Alice   Liddell  ", "Alice", "Liddell"},
		{"empty", "", "", ""},
		{"whitespace only", "   ", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, last := splitDisplayName(tt.in)
			if first != tt.wantFirst || last != tt.wantLast {
				t.Errorf("splitDisplayName(%q) = (%q, %q), want (%q, %q)",
					tt.in, first, last, tt.wantFirst, tt.wantLast)
			}
		})
	}
}
