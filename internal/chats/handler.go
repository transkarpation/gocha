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

	"github.com/transkarpation/gocha/internal/users"
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

type createRequest struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
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
	if req.Name == "" || len(req.Name) > maxNameLen {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("name must be 1-%d characters", maxNameLen))
		return
	}
	if req.Type != TypePublic && req.Type != TypeGroup {
		writeError(w, http.StatusUnprocessableEntity, `type must be "public" or "group"`)
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

	ids := make([]string, len(chat.Participants))
	for i, id := range chat.Participants {
		ids[i] = id.Hex()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":           chat.ID.Hex(),
		"name":         chat.Name,
		"type":         chat.Type,
		"participants": ids,
		"created_by":   chat.CreatedBy.Hex(),
		"created_at":   chat.CreatedAt,
	})
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

type sendMessageRequest struct {
	Text string `json:"text"`
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
	if req.Text == "" || len(req.Text) > maxMessageLen {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("text must be 1-%d characters", maxMessageLen))
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
