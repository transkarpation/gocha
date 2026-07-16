// Package mirror adapts external chat platforms to the users.ChatBackend
// interface. Swapping the chat provider or the mirroring transport means
// swapping the implementation here — the users package stays untouched.
package mirror

import (
	"context"
	"fmt"
	"log/slog"

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
// bypassed so mirrored users don't receive Ethora emails. Creation is
// asynchronous on the Ethora side — success means the job was accepted.
func (e *Ethora) MirrorUser(ctx context.Context, u users.User) error {
	bu, err := ethora.RandomBatchUser(u.Email, u.ID.Hex())
	if err != nil {
		return err
	}
	job, err := e.client.CreateUsersBatch(ctx, []ethora.BatchUser{bu}, true)
	if err != nil {
		return fmt.Errorf("create ethora user: %w", err)
	}
	slog.DebugContext(ctx, "ethora user creation accepted",
		"user_id", u.ID.Hex(), "job_id", job.JobID)
	return nil
}

// DeleteUser removes the mirrored Ethora account by our user id
// (Ethora knows it as the uuid set at creation time).
func (e *Ethora) DeleteUser(ctx context.Context, userID string) error {
	if err := e.client.DeleteUsersBatch(ctx, []string{userID}); err != nil {
		return fmt.Errorf("delete ethora user: %w", err)
	}
	return nil
}
