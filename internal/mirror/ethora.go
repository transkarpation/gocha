// Package mirror adapts external chat platforms to the users.ChatBackend
// interface. Swapping the chat provider or the mirroring transport means
// swapping the implementation here — the users package stays untouched.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/transkarpation/gocha/internal/users"
	"github.com/transkarpation/gocha/pkg/ethora"
)

type Ethora struct {
	client *ethora.Client
}

func NewEthora(client *ethora.Client) *Ethora {
	return &Ethora{client: client}
}

// MirrorUser creates the user in Ethora: our email and user id, random
// names and a one-off password (never stored — the server talks to Ethora
// with its app JWT, not with user credentials). Email confirmation is
// bypassed so mirrored users don't receive Ethora emails.
func (e *Ethora) MirrorUser(ctx context.Context, u users.User) error {
	bu, err := ethora.RandomBatchUser(u.Email, u.ID.Hex())
	if err != nil {
		return err
	}
	created, err := e.client.CreateUsersBatch(ctx, []ethora.BatchUser{bu}, true)
	if err != nil {
		return fmt.Errorf("create ethora user: %w", err)
	}
	ethoraID := ""
	if len(created) > 0 {
		ethoraID = created[0].ID
	}
	slog.InfoContext(ctx, "user created on ethora",
		"user_id", u.ID.Hex(), "email", u.Email, "ethora_id", ethoraID)
	return nil
}

// DeleteUser removes the mirrored Ethora account by our user id
// (Ethora knows it as the uuid set at creation time). A 404 counts as
// success — the account is already gone, which is the desired state.
func (e *Ethora) DeleteUser(ctx context.Context, userID string) error {
	err := e.client.DeleteUsersBatch(ctx, []string{userID})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete ethora user %s: %w", userID, err)
	}
	return nil
}

// DeleteUsers removes many mirrored Ethora accounts. It tries one batch
// call first; Ethora fails the whole batch when any id is unknown (users
// created before mirroring existed), so on failure it falls back to
// per-id deletes and only reports ids that genuinely failed.
func (e *Ethora) DeleteUsers(ctx context.Context, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	// A 404 on a multi-id batch means "some id unknown, nothing deleted",
	// so it must go through the fallback, not count as success.
	err := e.client.DeleteUsersBatch(ctx, userIDs)
	if err == nil || (len(userIDs) == 1 && isNotFound(err)) {
		return nil
	}

	var errs []error
	for _, id := range userIDs {
		if err := e.DeleteUser(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func isNotFound(err error) bool {
	var apiErr *ethora.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}
