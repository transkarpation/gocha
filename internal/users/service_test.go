package users

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/transkarpation/gocha/internal/permissions"
	"github.com/transkarpation/gocha/internal/testutil"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	db := testutil.MongoDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := NewStorage(ctx, db)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	return s
}

func TestRegister(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	u, err := Register(ctx, s, nil, "alice@example.com", "secret123", permissions.RoleUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.ID.IsZero() {
		t.Error("expected non-zero user id")
	}
	if u.Role != permissions.RoleUser {
		t.Errorf("role = %q, want %q", u.Role, permissions.RoleUser)
	}
	if u.PasswordHash == "secret123" || u.PasswordHash == "" {
		t.Error("password must be stored hashed")
	}

	tests := []struct {
		name     string
		email    string
		password string
		role     permissions.Role
		wantErr  error
	}{
		{"duplicate email", "alice@example.com", "secret123", permissions.RoleUser, ErrEmailTaken},
		{"invalid email", "not-an-email", "secret123", permissions.RoleUser, ErrInvalidEmail},
		{"short password", "bob@example.com", "1234567", permissions.RoleUser, ErrPasswordTooShort},
		{"invalid role", "bob@example.com", "secret123", "superuser", ErrInvalidRole},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Register(ctx, s, nil, tt.email, tt.password, tt.role); !errors.Is(err, tt.wantErr) {
				t.Errorf("Register() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	created, err := Register(ctx, s, nil, "alice@example.com", "secret123", permissions.RoleUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	u, err := Login(ctx, s, "alice@example.com", "secret123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if u.ID != created.ID {
		t.Errorf("logged in as %s, want %s", u.ID.Hex(), created.ID.Hex())
	}

	if _, err := Login(ctx, s, "alice@example.com", "wrongpass"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := Login(ctx, s, "nobody@example.com", "secret123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown email: error = %v, want ErrInvalidCredentials", err)
	}
}

func TestSessions(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	u, err := Register(ctx, s, nil, "alice@example.com", "secret123", permissions.RoleUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	sess, err := IssueSession(ctx, s, u)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if len(sess.Token) != 64 {
		t.Errorf("token length = %d, want 64 hex chars", len(sess.Token))
	}

	got, err := s.SessionByToken(ctx, sess.Token)
	if err != nil {
		t.Fatalf("SessionByToken: %v", err)
	}
	if got.UserID != u.ID {
		t.Errorf("session user = %s, want %s", got.UserID.Hex(), u.ID.Hex())
	}

	if _, err := s.SessionByToken(ctx, "forged-token"); !errors.Is(err, ErrNotFound) {
		t.Errorf("forged token: error = %v, want ErrNotFound", err)
	}

	expired, err := s.CreateSession(ctx, u.ID, "expired-token", -time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.SessionByToken(ctx, expired.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired token: error = %v, want ErrNotFound", err)
	}
}

type fakeChat struct {
	mirrored []User
	deleted  []string
	fail     bool
}

func (f *fakeChat) MirrorUser(_ context.Context, u User) error {
	if f.fail {
		return errors.New("chat backend down")
	}
	f.mirrored = append(f.mirrored, u)
	return nil
}

func (f *fakeChat) DeleteUser(_ context.Context, userID string) error {
	if f.fail {
		return errors.New("chat backend down")
	}
	f.deleted = append(f.deleted, userID)
	return nil
}

func TestRegisterMirrorsToChatBackend(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	chat := &fakeChat{}
	u, err := Register(ctx, s, chat, "alice@example.com", "secret123", permissions.RoleUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(chat.mirrored) != 1 || chat.mirrored[0].ID != u.ID {
		t.Errorf("mirrored = %+v, want the registered user", chat.mirrored)
	}

	// A failing mirror must not fail the registration (best-effort policy).
	failing := &fakeChat{fail: true}
	if _, err := Register(ctx, s, failing, "bob@example.com", "secret123", permissions.RoleUser); err != nil {
		t.Errorf("Register with failing mirror: %v, want success", err)
	}

	// A validation error must not reach the chat backend.
	before := len(chat.mirrored)
	if _, err := Register(ctx, s, chat, "alice@example.com", "secret123", permissions.RoleUser); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate register: %v, want ErrEmailTaken", err)
	}
	if len(chat.mirrored) != before {
		t.Error("failed registration must not be mirrored")
	}
}

func TestDeleteUser(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	chat := &fakeChat{}

	u, err := Register(ctx, s, chat, "doomed@example.com", "secret123", permissions.RoleUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	sess, err := IssueSession(ctx, s, u)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	if err := DeleteUser(ctx, s, chat, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := s.UserByID(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("user still exists after delete: %v", err)
	}
	if _, err := s.SessionByToken(ctx, sess.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("session still valid after delete: %v", err)
	}
	if len(chat.deleted) != 1 || chat.deleted[0] != u.ID.Hex() {
		t.Errorf("chat deleted = %v, want [%s]", chat.deleted, u.ID.Hex())
	}

	// Deleting again reports not found and must not reach the chat backend.
	before := len(chat.deleted)
	if err := DeleteUser(ctx, s, chat, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v, want ErrNotFound", err)
	}
	if len(chat.deleted) != before {
		t.Error("failed delete must not be mirrored")
	}

	// A failing chat backend must not fail the deletion.
	u2, err := Register(ctx, s, nil, "doomed2@example.com", "secret123", permissions.RoleUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := DeleteUser(ctx, s, &fakeChat{fail: true}, u2.ID); err != nil {
		t.Errorf("DeleteUser with failing chat backend: %v, want success", err)
	}
}

func TestLegacyUserRoleNormalization(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Users created before roles existed have no role field at all.
	if _, err := s.users.InsertOne(ctx, map[string]any{
		"email":         "legacy@example.com",
		"password_hash": "x",
	}); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}

	u, err := s.UserByEmail(ctx, "legacy@example.com")
	if err != nil {
		t.Fatalf("UserByEmail: %v", err)
	}
	if u.Role != permissions.RoleUser {
		t.Errorf("legacy role = %q, want %q", u.Role, permissions.RoleUser)
	}
}
