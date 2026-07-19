package validate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type sample struct {
	Email       string  `json:"email" validate:"required,email"`
	Password    string  `json:"password" validate:"required,min=8"`
	DisplayName *string `json:"display_name" validate:"omitempty,max=4"`
	Kind        string  `json:"kind" validate:"required,oneof=public group"`
}

func valid() sample {
	return sample{Email: "a@b.com", Password: "secret123", Kind: "public"}
}

func TestStructValid(t *testing.T) {
	if err := Struct(valid()); err != nil {
		t.Fatalf("Struct() = %v, want nil", err)
	}
}

func TestStructReportsJSONFieldNames(t *testing.T) {
	// The whole point of the tag-name hook: a client that sent
	// `display_name` must not be told that `DisplayName` is wrong.
	long := "toolong"
	s := valid()
	s.DisplayName = &long

	err := Struct(s)
	verrs, ok := err.(*Errors)
	if !ok {
		t.Fatalf("Struct() = %T (%v), want *Errors", err, err)
	}
	if _, ok := verrs.Fields["display_name"]; !ok {
		t.Errorf("fields = %v, want a display_name key", verrs.Fields)
	}
	if _, ok := verrs.Fields["DisplayName"]; ok {
		t.Error("fields must be keyed by JSON name, not the Go field name")
	}
}

func TestStructOmitemptySkipsAbsentPointer(t *testing.T) {
	// A nil pointer means "not sent" in a partial update, so its rules
	// must not fire — otherwise every PATCH would demand every field.
	if err := Struct(valid()); err != nil {
		t.Fatalf("nil display_name rejected: %v", err)
	}
}

func TestStructMessages(t *testing.T) {
	err := Struct(sample{})
	verrs, ok := err.(*Errors)
	if !ok {
		t.Fatalf("Struct() = %T, want *Errors", err)
	}

	want := map[string]string{
		"email":    "is required",
		"password": "is required",
		"kind":     "is required",
	}
	for field, msg := range want {
		if verrs.Fields[field] != msg {
			t.Errorf("fields[%q] = %q, want %q", field, verrs.Fields[field], msg)
		}
	}
	if len(verrs.Fields) != len(want) {
		t.Errorf("fields = %v, want exactly %d entries", verrs.Fields, len(want))
	}
}

func TestErrorStringIsStable(t *testing.T) {
	// Error() joins a map, whose iteration order is random; a flapping
	// message would make logs and tests unreliable.
	err := Struct(sample{}).(*Errors)
	first := err.Error()
	for range 20 {
		if got := err.Error(); got != first {
			t.Fatalf("Error() not stable: %q vs %q", got, first)
		}
	}
	if first != "email is required; kind is required; password is required" {
		t.Errorf("Error() = %q", first)
	}
}

func TestOneofMessageListsOptions(t *testing.T) {
	s := valid()
	s.Kind = "secret"
	err := Struct(s).(*Errors)
	if got, want := err.Fields["kind"], "must be one of: public, group"; got != want {
		t.Errorf("fields[kind] = %q, want %q", got, want)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	if !WriteError(w, Struct(sample{})) {
		t.Fatal("WriteError() = false, want true for a validation error")
	}
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var body struct {
		Error  string            `json:"error"`
		Fields map[string]string `json:"fields"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// Existing clients read `error`; it must stay a non-empty string.
	if body.Error == "" {
		t.Error("response must keep a non-empty `error` string for existing clients")
	}
	if body.Fields["email"] != "is required" {
		t.Errorf("fields = %v, want email->is required", body.Fields)
	}
}

func TestWriteErrorIgnoresNilAndForeignErrors(t *testing.T) {
	w := httptest.NewRecorder()
	if WriteError(w, nil) {
		t.Error("WriteError(nil) = true, want false")
	}
	if w.Body.Len() != 0 {
		t.Error("WriteError(nil) must not write a body")
	}

	// A non-validation error is the caller's problem: reporting it as 422
	// would dress an internal failure up as client error.
	w = httptest.NewRecorder()
	if WriteError(w, http.ErrBodyNotAllowed) {
		t.Error("WriteError(non-validation error) = true, want false")
	}
	if w.Body.Len() != 0 {
		t.Error("WriteError must not write a body for a foreign error")
	}
}
