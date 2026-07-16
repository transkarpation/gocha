package users

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/transkarpation/gocha/internal/permissions"
)

const (
	sessionTTL        = 24 * time.Hour
	sessionCookieName = "session"
	minPasswordLen    = 8
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
