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

// ParseToken verifies a token issued by IssueToken and returns the user id
// it identifies. Only HS256 is accepted — without pinning the algorithm an
// attacker could hand us a token signed with "none" or with the public half
// of an asymmetric key. Expiry is enforced by the parser.
//
// The claims are not trusted beyond the subject: role and email are read
// from storage, so a stale token cannot carry stale permissions.
func ParseToken(token string, secret []byte) (bson.ObjectID, error) {
	if len(secret) == 0 {
		return bson.ObjectID{}, ErrNoJWTSecret
	}
	var claims jwt.RegisteredClaims
	parsed, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !parsed.Valid {
		return bson.ObjectID{}, ErrInvalidToken
	}
	id, err := bson.ObjectIDFromHex(claims.Subject)
	if err != nil {
		return bson.ObjectID{}, ErrInvalidToken
	}
	return id, nil
}
