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
)

const (
	sessionTTL        = 24 * time.Hour
	sessionCookieName = "session"
	minPasswordLen    = 8

	defaultUsersLimit = 50
	maxUsersLimit     = 100
)

type Handler struct {
	storage *Storage
	chat    ChatBackend // nil disables mirroring
}

func NewHandler(storage *Storage, chat ChatBackend) *Handler {
	return &Handler{storage: storage, chat: chat}
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	// HTTP self-registration always creates a plain user;
	// admins are created via gochactrl.
	u, err := Register(r.Context(), h.storage, h.chat, req.Email, req.Password, permissions.RoleUser)
	switch {
	case errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrPasswordTooShort):
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

	h.respondWithSession(w, r, u, http.StatusCreated)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
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

	h.respondWithSession(w, r, u, http.StatusOK)
}

type updateUserRequest struct {
	Email    *string           `json:"email"`
	Password *string           `json:"password"`
	Role     *permissions.Role `json:"role"`
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

	u, err := UpdateUser(r.Context(), h.storage, id, UpdateUserParams{
		Email:    req.Email,
		Password: req.Password,
		Role:     req.Role,
	})
	switch {
	case errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrPasswordTooShort),
		errors.Is(err, ErrInvalidRole), errors.Is(err, ErrNothingToUpdate):
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
	json.NewEncoder(w).Encode(map[string]any{
		"id":         u.ID.Hex(),
		"email":      u.Email,
		"role":       u.Role,
		"created_at": u.CreatedAt,
	})
}

// Delete removes a user (admin permission is enforced by the route).
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := bson.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid user id")
		return
	}

	switch err := DeleteUser(r.Context(), h.storage, h.chat, id); {
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
		out[i] = map[string]any{
			"id":         u.ID.Hex(),
			"email":      u.Email,
			"role":       u.Role,
			"created_at": u.CreatedAt,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"users": out})
}

// Me returns the user resolved by the Auth middleware.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	u, ok := FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":    u.ID.Hex(),
		"email": u.Email,
		"role":  u.Role,
	})
}

// respondWithSession creates a session for the user, sets the cookie
// and writes the JSON response. Shared by Register and Login.
func (h *Handler) respondWithSession(w http.ResponseWriter, r *http.Request, u User, status int) {
	sess, err := IssueSession(r.Context(), h.storage, u)
	if err != nil {
		slog.ErrorContext(r.Context(), "issue session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.Token,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"id":            u.ID.Hex(),
		"email":         u.Email,
		"session_token": sess.Token,
	})
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
