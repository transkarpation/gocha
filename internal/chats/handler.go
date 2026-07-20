package chats

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/transkarpation/gocha/internal/permissions"
	"github.com/transkarpation/gocha/internal/users"
	"github.com/transkarpation/gocha/internal/validate"
)

const (
	maxNameLen    = 100
	maxMessageLen = 2000

	defaultMessagesLimit = 50
	maxMessagesLimit     = 100
)

type Handler struct {
	storage *Storage
	users   *users.Storage
}

func NewHandler(storage *Storage, userStorage *users.Storage) *Handler {
	return &Handler{storage: storage, users: userStorage}
}

// createRequest is validated after Name is trimmed: `required` rejects the
// empty string, but "   " would sail through it untrimmed.
// Participants are deliberately untagged — parseParticipants already
// resolves and reports them, and two sources of truth for the same field
// would drift.
type createRequest struct {
	Name         string   `json:"name" validate:"required,max=100"`
	Type         string   `json:"type" validate:"required,oneof=public group"`
	Participants []string `json:"participants"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	creator, ok := users.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if validate.WriteError(w, validate.Struct(req)) {
		return
	}

	participants, err := parseParticipants(req.Participants, creator.ID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if req.Type == TypeGroup && len(participants) < 2 {
		writeError(w, http.StatusUnprocessableEntity, "group chat needs at least one participant besides the creator")
		return
	}

	count, err := h.users.CountExisting(r.Context(), participants)
	if err != nil {
		slog.ErrorContext(r.Context(), "count participants", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if count != int64(len(participants)) {
		writeError(w, http.StatusUnprocessableEntity, "some participants do not exist")
		return
	}

	chat, err := h.storage.Create(r.Context(), Chat{
		Name:         req.Name,
		Type:         req.Type,
		Participants: participants,
		CreatedBy:    creator.ID,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "create chat", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(chatJSON(chat))
}

// chatJSON is the public shape of a chat — the one place deciding which
// fields leave the handler, shared by Create and AddParticipants.
func chatJSON(c Chat) map[string]any {
	ids := make([]string, len(c.Participants))
	for i, id := range c.Participants {
		ids[i] = id.Hex()
	}
	return map[string]any{
		"id":           c.ID.Hex(),
		"name":         c.Name,
		"type":         c.Type,
		"participants": ids,
		"created_by":   c.CreatedBy.Hex(),
		"created_at":   c.CreatedAt,
	}
}

// addParticipantsRequest carries the ids to add. Like createRequest the
// slice is untagged: parseIDs resolves and reports each id itself.
type addParticipantsRequest struct {
	Participants []string `json:"participants"`
}

// AddParticipants grows a chat's roster. The route requires chats:update,
// which plain users have too, so the real gate is here: only the chat's
// creator or an admin may change who is in it. Adding someone already in
// the chat is a no-op ($addToSet), which keeps the call idempotent.
func (h *Handler) AddParticipants(w http.ResponseWriter, r *http.Request) {
	caller, ok := users.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := bson.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid chat id")
		return
	}

	chat, err := h.storage.ChatByID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "load chat", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Being a participant is not enough: a member must not be able to pull
	// strangers into someone else's chat.
	if chat.CreatedBy != caller.ID && !permissions.Has(caller.Role, permissions.ChatsModerate) {
		writeError(w, http.StatusForbidden, "only the chat creator can add participants")
		return
	}

	var req addParticipantsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ids, err := parseIDs(req.Participants)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(ids) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "participants must not be empty")
		return
	}

	count, err := h.users.CountExisting(r.Context(), ids)
	if err != nil {
		slog.ErrorContext(r.Context(), "count participants", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if count != int64(len(ids)) {
		writeError(w, http.StatusUnprocessableEntity, "some participants do not exist")
		return
	}

	updated, err := h.storage.AddParticipants(r.Context(), id, ids)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "add participants", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chatJSON(updated))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := bson.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid chat id")
		return
	}

	switch err := h.storage.Delete(r.Context(), id); {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		slog.ErrorContext(r.Context(), "delete chat", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// sendMessageRequest is validated after Text is trimmed, same as
// createRequest.
type sendMessageRequest struct {
	Text string `json:"text" validate:"required,max=2000"`
}

func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	chat, sender, ok := h.chatForRequest(w, r)
	if !ok {
		return
	}

	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if validate.WriteError(w, validate.Struct(req)) {
		return
	}

	msg, err := h.storage.CreateMessage(r.Context(), Message{
		ChatID:   chat.ID,
		AuthorID: sender.ID,
		Text:     req.Text,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "create message", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(messageJSON(msg))
}

func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	chat, _, ok := h.chatForRequest(w, r)
	if !ok {
		return
	}

	limit := int64(defaultMessagesLimit)
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 || n > maxMessagesLimit {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("limit must be 1-%d", maxMessagesLimit))
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

	messages, err := h.storage.MessagesByChat(r.Context(), chat.ID, limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "list messages", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]map[string]any, len(messages))
	for i, m := range messages {
		out[i] = messageJSON(m)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"messages": out})
}

// chatForRequest loads the chat from the {id} url param and checks that
// the authenticated user may access it: group chats are participants-only,
// public chats are open to any authenticated user. On failure it writes
// the error response and returns ok=false.
func (h *Handler) chatForRequest(w http.ResponseWriter, r *http.Request) (Chat, users.User, bool) {
	u, ok := users.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return Chat{}, users.User{}, false
	}

	id, err := bson.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid chat id")
		return Chat{}, users.User{}, false
	}

	chat, err := h.storage.ChatByID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return Chat{}, users.User{}, false
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "load chat", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return Chat{}, users.User{}, false
	}

	if chat.Type == TypeGroup && !isParticipant(chat, u.ID) {
		writeError(w, http.StatusForbidden, "not a chat participant")
		return Chat{}, users.User{}, false
	}
	return chat, u, true
}

func isParticipant(c Chat, userID bson.ObjectID) bool {
	for _, id := range c.Participants {
		if id == userID {
			return true
		}
	}
	return false
}

func messageJSON(m Message) map[string]any {
	return map[string]any{
		"id":         m.ID.Hex(),
		"chat_id":    m.ChatID.Hex(),
		"author_id":  m.AuthorID.Hex(),
		"text":       m.Text,
		"created_at": m.CreatedAt,
	}
}

// parseParticipants converts hex ids to ObjectIDs, removes duplicates
// and makes sure the creator is always included.
// parseIDs converts hex ids to ObjectIDs and removes duplicates, without
// injecting anyone — unlike parseParticipants, which always adds the
// creator because a chat cannot exist without them.
func parseIDs(raw []string) ([]bson.ObjectID, error) {
	seen := map[bson.ObjectID]bool{}
	out := []bson.ObjectID{}
	for _, s := range raw {
		id, err := bson.ObjectIDFromHex(s)
		if err != nil {
			return nil, fmt.Errorf("invalid participant id: %q", s)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func parseParticipants(raw []string, creator bson.ObjectID) ([]bson.ObjectID, error) {
	seen := map[bson.ObjectID]bool{creator: true}
	out := []bson.ObjectID{creator}
	for _, s := range raw {
		id, err := bson.ObjectIDFromHex(s)
		if err != nil {
			return nil, fmt.Errorf("invalid participant id: %q", s)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
