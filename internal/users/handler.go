package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/transkarpation/gocha/internal/permissions"
	"github.com/transkarpation/gocha/internal/validate"
)

const (
	// TokenTTL is how long an issued access token stays valid.
	TokenTTL = 24 * time.Hour

	accessTokenCookieName = "access_token"
	minPasswordLen        = 8
	maxDisplayNameLen     = 64

	defaultUsersLimit = 50
	maxUsersLimit     = 100
)

type Handler struct {
	storage   *Storage
	chat      ChatBackend // nil disables mirroring
	jwtSecret []byte      // signs the tokens Register/Login hand out
}

func NewHandler(storage *Storage, chat ChatBackend, jwtSecret []byte) *Handler {
	return &Handler{storage: storage, chat: chat, jwtSecret: jwtSecret}
}

// registerRequest carries the sign-up payload. display_name is optional —
// omitting it keeps the account nameless, which is what the CLI and
// pre-existing API clients do.
//
// The tag limits must stay in step with the service-layer constants they
// mirror; TestTagsMatchServiceLimits fails if they drift apart.
type registerRequest struct {
	Email       string `json:"email" validate:"required,email"`
	Password    string `json:"password" validate:"required,min=8"`
	DisplayName string `json:"display_name" validate:"omitempty,max=64"`
}

// loginRequest deliberately does NOT constrain the password beyond being
// present: a wrong password must come back as 401 from the credential
// check, never as a 422 that reveals the password policy.
type loginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if validate.WriteError(w, validate.Struct(req)) {
		return
	}

	// HTTP self-registration always creates a plain user;
	// admins are created via gochactrl.
	u, err := Register(r.Context(), h.storage, h.chat, RegisterParams{
		Email:       req.Email,
		Password:    req.Password,
		Role:        permissions.RoleUser,
		DisplayName: req.DisplayName,
	})
	switch {
	case errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrPasswordTooShort),
		errors.Is(err, ErrDisplayNameTooLong):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case errors.Is(err, ErrEmailTaken):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "register user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.respondWithToken(w, r, u, http.StatusCreated)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if validate.WriteError(w, validate.Struct(req)) {
		return
	}

	u, err := Login(r.Context(), h.storage, req.Email, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "login user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.respondWithToken(w, r, u, http.StatusOK)
}

// userResponse is the public JSON shape of a user — the one place that
// decides which fields leave the handler (password hashes never do).
// display_name is always present, empty for accounts that have none, so
// clients can rely on the key existing.
func userResponse(u User) map[string]any {
	return map[string]any{
		"id":           u.ID.Hex(),
		"email":        u.Email,
		"display_name": u.DisplayName,
		"role":         u.Role,
		"created_at":   u.CreatedAt,
	}
}

// updateUserRequest is a partial update: every field is a pointer, and a
// nil one is absent rather than empty. `omitempty` makes each rule apply
// only when the field was actually sent — except display_name, where an
// explicit "" is a meaningful value (it clears the name), so only its
// length is constrained.
type updateUserRequest struct {
	Email       *string           `json:"email" validate:"omitempty,email"`
	Password    *string           `json:"password" validate:"omitempty,min=8"`
	Role        *permissions.Role `json:"role" validate:"omitempty,oneof=admin user"`
	DisplayName *string           `json:"display_name" validate:"omitempty,max=64"`
}

// Update partially updates a user (admin permission is enforced by the
// route). Changing the password logs the user out everywhere.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := bson.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid user id")
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if validate.WriteError(w, validate.Struct(req)) {
		return
	}

	u, err := UpdateUser(r.Context(), h.storage, id, UpdateUserParams{
		Email:       req.Email,
		Password:    req.Password,
		Role:        req.Role,
		DisplayName: req.DisplayName,
	})
	switch {
	case errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrPasswordTooShort),
		errors.Is(err, ErrInvalidRole), errors.Is(err, ErrNothingToUpdate),
		errors.Is(err, ErrDisplayNameTooLong):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case errors.Is(err, ErrEmailTaken):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "user not found")
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "update user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userResponse(u))
}

// Delete soft-deletes a user (admin permission is enforced by the route).
// The Ethora mirror is kept; permanent removal is gochactrl-only.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := bson.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid user id")
		return
	}

	switch err := DeleteUser(r.Context(), h.storage, id); {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	case err != nil:
		slog.ErrorContext(r.Context(), "delete user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// List returns users, oldest first (admin permission is enforced by the
// route). Password hashes never leave the handler.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	limit := int64(defaultUsersLimit)
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 || n > maxUsersLimit {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("limit must be 1-%d", maxUsersLimit))
			return
		}
		limit = n
	}
	offset := int64(0)
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusUnprocessableEntity, "offset must be >= 0")
			return
		}
		offset = n
	}

	list, err := h.storage.ListUsers(r.Context(), limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "list users", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]map[string]any, len(list))
	for i, u := range list {
		out[i] = userResponse(u)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"users": out})
}

// Restore brings a soft-deleted user back (admin permission is enforced
// by the route).
func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	id, err := bson.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid user id")
		return
	}

	u, err := RestoreUser(r.Context(), h.storage, id)
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "user not found")
		return
	case errors.Is(err, ErrNotDeleted):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "restore user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userResponse(u))
}

// Me returns the user resolved by the Auth middleware.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	u, ok := FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userResponse(u))
}

// respondWithToken signs an access token for the user, sets the cookie
// and writes the JSON response. Shared by Register and Login.
//
// The response also carries the XMPP credentials of the user's mirrored
// chat account so the client can connect to the XMPP server itself. They
// are omitted when the user has none (mirroring disabled or it failed at
// registration) — that must not break signing in.
func (h *Handler) respondWithToken(w http.ResponseWriter, r *http.Request, u User, status int) {
	token, err := IssueToken(u, h.jwtSecret, TokenTTL)
	if err != nil {
		slog.ErrorContext(r.Context(), "issue access token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	expiresAt := time.Now().UTC().Add(TokenTTL)

	// Browsers get the token in an HttpOnly cookie so page scripts cannot
	// read it; everyone else sends it as a Bearer header.
	http.SetCookie(w, &http.Cookie{
		Name:     accessTokenCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	body := map[string]any{
		"id":           u.ID.Hex(),
		"email":        u.Email,
		"display_name": u.DisplayName,
		"access_token": token,
		"expires_at":   expiresAt,
	}
	switch creds, err := h.storage.ChatCredentialsByUserID(r.Context(), u.ID); {
	case err == nil:
		body["xmpp_username"] = creds.XMPPUsername
		body["xmpp_password"] = creds.XMPPPassword
	case !errors.Is(err, ErrNotFound):
		slog.ErrorContext(r.Context(), "load chat credentials",
			"user_id", u.ID.Hex(), "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
