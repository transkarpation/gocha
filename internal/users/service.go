package users

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/mail"

	"golang.org/x/crypto/bcrypt"

	"github.com/transkarpation/gocha/internal/permissions"
)

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	ErrInvalidRole        = errors.New(`role must be "admin" or "user"`)
	ErrInvalidCredentials = errors.New("invalid email or password")
)

// Register validates credentials, hashes the password and stores the user.
// Shared by the HTTP handler and the backendctrl CLI.
func Register(ctx context.Context, s *Storage, email, password string, role permissions.Role) (User, error) {
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
	return s.CreateUser(ctx, email, string(hash), role)
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
// Shared by the HTTP handlers and the backendctrl CLI.
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
