package ethora

import (
	"context"
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
