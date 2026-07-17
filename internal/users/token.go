package users

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrNoJWTSecret is returned when token issuance is attempted without a
// signing key: an empty HS256 key would make tokens trivially forgeable.
var ErrNoJWTSecret = errors.New("jwt secret is not configured")

// IssueToken signs a JWT identifying the user, valid for ttl. The claims
// carry our user id (sub), email and role — everything a client needs to
// know about itself without another round trip.
//
// This is our own token (signed with auth.jwt_secret), unrelated to the
// server-to-server JWT pkg/ethora signs with the Ethora API secret.
func IssueToken(u User, secret []byte, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", ErrNoJWTSecret
	}
	now := time.Now().UTC()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   u.ID.Hex(),
		"email": u.Email,
		"role":  string(u.Role),
		"iat":   now.Unix(),
		"exp":   now.Add(ttl).Unix(),
	})
	return tok.SignedString(secret)
}
