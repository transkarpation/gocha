package users

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var (
	// ErrNoJWTSecret is returned when signing or verification is attempted
	// without a key: an empty HS256 key would make tokens trivially
	// forgeable, so it is a configuration error, not an auth failure.
	ErrNoJWTSecret = errors.New("jwt secret is not configured")

	// ErrInvalidToken covers every reason an access token is not usable:
	// bad signature, wrong algorithm, expired, malformed subject.
	ErrInvalidToken = errors.New("invalid or expired token")
)

// accessClaims are the claims of an access token: the registered set plus
// the token version that makes revocation possible.
type accessClaims struct {
	Email   string `json:"email,omitempty"`
	Role    string `json:"role,omitempty"`
	Version int    `json:"ver"`
	jwt.RegisteredClaims
}

// IssueToken signs a JWT identifying the user, valid for ttl. The claims
// carry our user id (sub), email and role — everything a client needs to
// know about itself without another round trip — plus the user's current
// token version.
//
// This is our own token (signed with auth.jwt_secret), unrelated to the
// server-to-server JWT pkg/ethora signs with the Ethora API secret.
func IssueToken(u User, secret []byte, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", ErrNoJWTSecret
	}
	now := time.Now().UTC()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims{
		Email:   u.Email,
		Role:    string(u.Role),
		Version: u.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID.Hex(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	})
	return tok.SignedString(secret)
}

// TokenClaims is what Auth needs from a verified access token. Nothing
// else is trusted: role and email are read from storage, so a token minted
// before a demotion cannot carry stale permissions.
type TokenClaims struct {
	UserID  bson.ObjectID
	Version int
}

// ParseToken verifies a token issued by IssueToken. Only HS256 is accepted
// — without pinning the algorithm an attacker could hand us a token signed
// with "none" or with the public half of an asymmetric key. Expiry is
// enforced by the parser; the returned version is what the caller compares
// against the user's current one.
func ParseToken(token string, secret []byte) (TokenClaims, error) {
	if len(secret) == 0 {
		return TokenClaims{}, ErrNoJWTSecret
	}
	var claims accessClaims
	parsed, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !parsed.Valid {
		return TokenClaims{}, ErrInvalidToken
	}
	id, err := bson.ObjectIDFromHex(claims.Subject)
	if err != nil {
		return TokenClaims{}, ErrInvalidToken
	}
	return TokenClaims{UserID: id, Version: claims.Version}, nil
}
