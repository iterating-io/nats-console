package users

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/iterating-io/nats-console/api/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) updateUserPublishAllow(w http.ResponseWriter, r *http.Request, update func(string, string, string, string) (*store.User, error)) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	user := strings.TrimSpace(r.PathValue("user"))
	var req struct {
		Subject string `json:"subject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}
	u, err := update(operator, accountPublicKey, user, subject)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "subject already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) natsConn() NATSClient {
	if h.natsRef == nil {
		return nil
	}
	return h.natsRef()
}
