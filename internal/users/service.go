package users

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/mail"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"github.com/transkarpation/gocha/internal/permissions"
)

// ChatBackend replicates our accounts into an external chat platform
// (e.g. Ethora, see internal/mirror). A nil ChatBackend disables mirroring.
type ChatBackend interface {
	MirrorUser(ctx context.Context, u User) error
	// DeleteUser removes the mirrored account; userID is our user id in hex.
	DeleteUser(ctx context.Context, userID string) error
}

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	ErrInvalidRole        = errors.New(`role must be "admin" or "user"`)
	ErrInvalidCredentials = errors.New("invalid email or password")
)

// Register validates credentials, hashes the password and stores the user.
// Shared by the HTTP handler and the gochactrl CLI.
//
// When chat is non-nil the new user is also mirrored into the external chat
// platform. Mirroring is currently best-effort: a failure is logged and the
// registration still succeeds. To change that policy (fail registration,
// retry in background, ...) adjust this one spot.
func Register(ctx context.Context, s *Storage, chat ChatBackend, email, password string, role permissions.Role) (User, error) {
	if _, err := mail.ParseAddress(email); err != nil {
		return User{}, ErrInvalidEmail
	}
	if len(password) < minPasswordLen {
		return User{}, ErrPasswordTooShort
	}
	if !permissions.ValidRole(role) {
		return User{}, ErrInvalidRole
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u, err := s.CreateUser(ctx, email, string(hash), role)
	if err != nil {
		return User{}, err
	}

	if chat != nil {
		if err := chat.MirrorUser(ctx, u); err != nil {
			slog.WarnContext(ctx, "mirror user to chat backend",
				"user_id", u.ID.Hex(), "error", err)
		}
	}
	return u, nil
}

// DeleteUser removes the user, all their sessions and (best-effort, same
// policy as mirroring in Register) the mirrored chat account.
// Shared by the HTTP handler and the gochactrl CLI.
func DeleteUser(ctx context.Context, s *Storage, chat ChatBackend, id bson.ObjectID) error {
	if err := s.DeleteUser(ctx, id); err != nil {
		return err
	}
	if chat != nil {
		if err := chat.DeleteUser(ctx, id.Hex()); err != nil {
			slog.WarnContext(ctx, "delete user from chat backend",
				"user_id", id.Hex(), "error", err)
		}
	}
	return nil
}

// Login verifies the credentials and returns the matching user.
// A missing user and a wrong password are indistinguishable to the caller.
func Login(ctx context.Context, s *Storage, email, password string) (User, error) {
	u, err := s.UserByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return User{}, ErrInvalidCredentials
	}
	return u, nil
}

// IssueSession generates a fresh token and stores a session for the user.
// Shared by the HTTP handlers and the gochactrl CLI.
func IssueSession(ctx context.Context, s *Storage, u User) (Session, error) {
	token, err := newSessionToken()
	if err != nil {
		return Session{}, err
	}
	return s.CreateSession(ctx, u.ID, token, sessionTTL)
}

func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
