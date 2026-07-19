package users

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"github.com/transkarpation/gocha/internal/permissions"
)

// ChatAccount is what the chat backend hands back after mirroring: the
// XMPP credentials our server can use to act as that user. A zero value
// means the backend has none to report.
type ChatAccount struct {
	XMPPUsername string
	XMPPPassword string
}

// ChatBackend replicates our accounts into an external chat platform
// (e.g. Ethora, see internal/mirror). A nil ChatBackend disables mirroring.
type ChatBackend interface {
	MirrorUser(ctx context.Context, u User) (ChatAccount, error)
	// DeleteUser removes the mirrored account; userID is our user id in hex.
	DeleteUser(ctx context.Context, userID string) error
	// DeleteUsers removes many mirrored accounts in one call.
	DeleteUsers(ctx context.Context, userIDs []string) error
}

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	ErrInvalidRole        = errors.New(`role must be "admin" or "user"`)
	ErrDisplayNameTooLong = fmt.Errorf("display name must be at most %d characters", maxDisplayNameLen)
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNothingToUpdate    = errors.New("nothing to update")
	ErrSystemExists       = errors.New("system account already exists")
)

// SystemEmail identifies the server's own account — service messages are
// sent on its behalf. The account is created at server startup with a
// random throwaway password, so nobody can log in as it.
const SystemEmail = "system@gocha.internal"

// SystemDisplayName is what clients render for service messages.
const SystemDisplayName = "System"

// EnsureSystemUser makes sure the system account exists and is alive:
// missing — registered (and mirrored like any user), soft-deleted —
// restored, present — returned as is. Called once at server startup,
// safe to call repeatedly.
func EnsureSystemUser(ctx context.Context, s *Storage, chat ChatBackend) (User, error) {
	u, err := s.AnyUserByEmail(ctx, SystemEmail)
	switch {
	case err == nil && u.DeletedAt == nil:
		return u, nil
	case err == nil: // soft-deleted, e.g. by an admin — bring it back
		u, err = s.RestoreUser(ctx, u.ID)
		if err != nil {
			return User{}, err
		}
		slog.InfoContext(ctx, "system user restored", "user_id", u.ID.Hex())
		return u, nil
	case errors.Is(err, ErrNotFound):
		return createSystemUser(ctx, s, chat)
	default:
		return User{}, err
	}
}

// InitSystemUser creates the system account and fails with ErrSystemExists
// when it is already there (soft-deleted counts as existing). Used by
// gochactrl init-system; the server startup path is the idempotent
// EnsureSystemUser.
func InitSystemUser(ctx context.Context, s *Storage, chat ChatBackend) (User, error) {
	_, err := s.AnyUserByEmail(ctx, SystemEmail)
	switch {
	case err == nil:
		return User{}, ErrSystemExists
	case errors.Is(err, ErrNotFound):
		return createSystemUser(ctx, s, chat)
	default:
		return User{}, err
	}
}

func createSystemUser(ctx context.Context, s *Storage, chat ChatBackend) (User, error) {
	// The password is never needed again: the server acts through
	// storage directly, not through login.
	password, err := randomSecret()
	if err != nil {
		return User{}, err
	}
	u, err := Register(ctx, s, chat, RegisterParams{
		Email:       SystemEmail,
		Password:    password,
		Role:        permissions.RoleUser,
		DisplayName: SystemDisplayName,
	})
	if err != nil {
		return User{}, err
	}
	slog.InfoContext(ctx, "system user created", "user_id", u.ID.Hex(), "email", u.Email)
	return u, nil
}

// RegisterParams are the inputs of Register. DisplayName is optional —
// the CLI and the system account leave it empty — everything else is
// required. A struct rather than positional arguments so a caller cannot
// silently swap two strings.
type RegisterParams struct {
	Email       string
	Password    string
	Role        permissions.Role
	DisplayName string
}

// Register validates credentials, hashes the password and stores the user.
// Shared by the HTTP handler and the gochactrl CLI.
//
// When chat is non-nil the new user is also mirrored into the external chat
// platform. Mirroring is currently best-effort: a failure is logged and the
// registration still succeeds. To change that policy (fail registration,
// retry in background, ...) adjust this one spot.
func Register(ctx context.Context, s *Storage, chat ChatBackend, p RegisterParams) (User, error) {
	if _, err := mail.ParseAddress(p.Email); err != nil {
		return User{}, ErrInvalidEmail
	}
	if len(p.Password) < minPasswordLen {
		return User{}, ErrPasswordTooShort
	}
	if !permissions.ValidRole(p.Role) {
		return User{}, ErrInvalidRole
	}
	displayName, err := normalizeDisplayName(p.DisplayName)
	if err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u, err := s.CreateUser(ctx, p.Email, string(hash), p.Role, displayName)
	if err != nil {
		return User{}, err
	}

	if chat != nil {
		acc, err := chat.MirrorUser(ctx, u)
		switch {
		case err != nil:
			slog.WarnContext(ctx, "mirror user to chat backend",
				"user_id", u.ID.Hex(), "error", err)
		case acc != (ChatAccount{}):
			// Same best-effort policy as the mirroring itself.
			if err := s.SaveChatCredentials(ctx, u.ID, acc.XMPPUsername, acc.XMPPPassword); err != nil {
				slog.WarnContext(ctx, "store chat credentials",
					"user_id", u.ID.Hex(), "error", err)
			}
		}
	}
	return u, nil
}

// UpdateUserParams is a partial update: nil fields stay unchanged.
type UpdateUserParams struct {
	Email       *string
	Password    *string
	Role        *permissions.Role
	DisplayName *string
}

// UpdateUser validates and applies the partial update. Changing the
// password invalidates all access tokens issued so far (storage moves
// tokens_valid_from in the same write).
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
	if p.DisplayName != nil {
		// An explicit "" clears the name — the field is optional, so
		// unsetting it is a legitimate update, not a no-op.
		name, err := normalizeDisplayName(*p.DisplayName)
		if err != nil {
			return User{}, err
		}
		upd.DisplayName = &name
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

	return s.UpdateUser(ctx, id, upd)
}

// DeleteUser soft-deletes the user: sets deleted_at, which also locks out
// their access tokens (Auth resolves every token against storage). The
// default deletion everywhere (HTTP and CLI). Deliberately does NOT touch
// the chat backend — the Ethora mirror survives a soft delete.
func DeleteUser(ctx context.Context, s *Storage, id bson.ObjectID) error {
	return s.SoftDeleteUser(ctx, id)
}

// RestoreUser brings a soft-deleted user back to life. Access tokens
// issued before the deletion start working again if they have not expired
// — restoring undoes the lockout by design. The Ethora mirror was never
// touched, so nothing to redo there.
// Shared by the HTTP handler and the gochactrl CLI.
func RestoreUser(ctx context.Context, s *Storage, id bson.ObjectID) (User, error) {
	return s.RestoreUser(ctx, id)
}

// HardDeleteUser permanently removes the user and (best-effort, same
// policy as mirroring in Register) the mirrored chat account.
// Only reachable via gochactrl delete --hard.
func HardDeleteUser(ctx context.Context, s *Storage, chat ChatBackend, id bson.ObjectID) error {
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

// DeleteAllUsers wipes every user and (best-effort) all mirrored chat
// accounts. The system account is never deleted — neither here nor its
// chat mirror. Returns the number of deleted users.
// Used by gochactrl only — there is deliberately no HTTP route for this.
func DeleteAllUsers(ctx context.Context, s *Storage, chat ChatBackend) (int64, error) {
	ids, err := s.AllUserIDs(ctx)
	if err != nil {
		return 0, err
	}
	sysID := ""
	if sys, err := s.AnyUserByEmail(ctx, SystemEmail); err == nil {
		sysID = sys.ID.Hex()
	} else if !errors.Is(err, ErrNotFound) {
		return 0, err
	}

	count, err := s.DeleteAllUsers(ctx)
	if err != nil {
		return count, err
	}

	if chat != nil {
		hexIDs := make([]string, 0, len(ids))
		for _, id := range ids {
			if id.Hex() != sysID {
				hexIDs = append(hexIDs, id.Hex())
			}
		}
		if len(hexIDs) > 0 {
			if err := chat.DeleteUsers(ctx, hexIDs); err != nil {
				slog.WarnContext(ctx, "delete users from chat backend",
					"count", len(hexIDs), "error", err)
			}
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

// normalizeDisplayName trims surrounding whitespace and enforces the length
// limit. An empty name is valid and means "not set": the field is optional,
// so trimming a whitespace-only name back to empty is not an error either.
// The limit counts runes, not bytes — otherwise a name in Cyrillic would be
// rejected at half the length of one in ASCII.
func normalizeDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxDisplayNameLen {
		return "", ErrDisplayNameTooLong
	}
	return name, nil
}

// randomSecret returns 32 random bytes as hex — used for the system
// account's throwaway password.
func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
