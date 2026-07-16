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
	// DeleteUsers removes many mirrored accounts in one call.
	DeleteUsers(ctx context.Context, userIDs []string) error
}

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	ErrInvalidRole        = errors.New(`role must be "admin" or "user"`)
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNothingToUpdate    = errors.New("nothing to update")
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

// UpdateUserParams is a partial update: nil fields stay unchanged.
type UpdateUserParams struct {
	Email    *string
	Password *string
	Role     *permissions.Role
}

// UpdateUser validates and applies the partial update. Changing the
// password invalidates all of the user's sessions.
func UpdateUser(ctx context.Context, s *Storage, id bson.ObjectID, p UpdateUserParams) (User, error) {
	upd := UserUpdate{Email: p.Email, Role: p.Role}
	if p.Email != nil {
		if _, err := mail.ParseAddress(*p.Email); err != nil {
			return User{}, ErrInvalidEmail
		}
	}
	if p.Role != nil && !permissions.ValidRole(*p.Role) {
		return User{}, ErrInvalidRole
	}
	if p.Password != nil {
		if len(*p.Password) < minPasswordLen {
			return User{}, ErrPasswordTooShort
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*p.Password), bcrypt.DefaultCost)
		if err != nil {
			return User{}, err
		}
		hs := string(hash)
		upd.PasswordHash = &hs
	}
	if upd == (UserUpdate{}) {
		return User{}, ErrNothingToUpdate
	}

	u, err := s.UpdateUser(ctx, id, upd)
	if err != nil {
		return User{}, err
	}
	if p.Password != nil {
		if err := s.DeleteSessions(ctx, u.ID); err != nil {
			return User{}, err
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

// DeleteAllUsers wipes every user and session and (best-effort) all
// mirrored chat accounts. Returns the number of deleted users.
// Used by gochactrl only — there is deliberately no HTTP route for this.
func DeleteAllUsers(ctx context.Context, s *Storage, chat ChatBackend) (int64, error) {
	ids, err := s.AllUserIDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	count, err := s.DeleteAllUsers(ctx)
	if err != nil {
		return count, err
	}

	if chat != nil {
		hexIDs := make([]string, len(ids))
		for i, id := range ids {
			hexIDs[i] = id.Hex()
		}
		if err := chat.DeleteUsers(ctx, hexIDs); err != nil {
			slog.WarnContext(ctx, "delete users from chat backend",
				"count", len(hexIDs), "error", err)
		}
	}
	return count, nil
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
