package ethora

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
)

type CreateUserRequest struct {
	UUID      string `json:"uuid"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email,omitempty"`
	Password  string `json:"password,omitempty"`
}

type User struct {
	ID           string `json:"_id"`
	UUID         string `json:"uuid"`
	AppID        string `json:"appId"`
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	XMPPPassword string `json:"xmppPassword"`
}

type createUserResponse struct {
	User User `json:"user"`
}

// CreateUser registers a user in the Ethora application.
func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (User, error) {
	var resp createUserResponse
	if err := c.do(ctx, http.MethodPost, "/v1/users", req, &resp); err != nil {
		return User{}, err
	}
	return resp.User, nil
}

type BatchUser struct {
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Password  string `json:"password"`
	UUID      string `json:"uuid"`
}

type batchCreateRequest struct {
	BypassEmailConfirmation bool        `json:"bypassEmailConfirmation"`
	UsersList               []BatchUser `json:"usersList"`
}

// BatchJob describes the asynchronous batch-creation job Ethora starts:
// the endpoint responds 202 and the job can be polled via StatusURL.
type BatchJob struct {
	JobID     string `json:"jobId"`
	StatusURL string `json:"statusUrl"`
}

// CreateUsersBatch registers users via POST /v2/users/batch.
// The call is asynchronous on the Ethora side — a successful response
// only means the job was accepted.
func (c *Client) CreateUsersBatch(ctx context.Context, users []BatchUser, bypassEmailConfirmation bool) (BatchJob, error) {
	req := batchCreateRequest{
		BypassEmailConfirmation: bypassEmailConfirmation,
		UsersList:               users,
	}
	var job BatchJob
	if err := c.do(ctx, http.MethodPost, "/v2/users/batch", req, &job); err != nil {
		return BatchJob{}, err
	}
	return job, nil
}

// RandomBatchUser builds a BatchUser for mirroring one of our accounts into
// Ethora: the given email and uuid (our server-side user id) plus random
// names and a random one-off password. The password exists only in this
// value — do not persist it anywhere.
func RandomBatchUser(email, uuid string) (BatchUser, error) {
	first, err := randomHex(4)
	if err != nil {
		return BatchUser{}, err
	}
	last, err := randomHex(4)
	if err != nil {
		return BatchUser{}, err
	}
	password, err := randomHex(16)
	if err != nil {
		return BatchUser{}, err
	}
	return BatchUser{
		Email:     email,
		FirstName: "user_" + first,
		LastName:  "user_" + last,
		Password:  password,
		UUID:      uuid,
	}, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return hex.EncodeToString(b), nil
}
