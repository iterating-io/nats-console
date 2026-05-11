package accounts

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) updateAllow(w http.ResponseWriter, r *http.Request, update func(string, string, string) (Record, bool, bool)) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	name := strings.TrimSpace(r.PathValue("name"))
	subject := decodeSubject(w, r)
	if subject == "" {
		return
	}
	updated, found, duplicate := update(operator, name, subject)
	if duplicate {
		writeError(w, http.StatusConflict, "subject already exists")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err := h.service.PushAccountToNATS(updated); err != nil {
		writeError(w, http.StatusBadGateway, "failed to update nats resolver")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) removeAllow(w http.ResponseWriter, r *http.Request, update func(string, string, string) (Record, bool)) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	name := strings.TrimSpace(r.PathValue("name"))
	subject := decodeSubject(w, r)
	if subject == "" {
		return
	}
	updated, found := update(operator, name, subject)
	if !found {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err := h.service.PushAccountToNATS(updated); err != nil {
		writeError(w, http.StatusBadGateway, "failed to update nats resolver")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func decodeSubject(w http.ResponseWriter, r *http.Request) string {
	var req struct {
		Subject string `json:"subject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return ""
	}
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return ""
	}
	return subject
}

func (h *Handler) refreshNATSCapabilitiesIfStale(maxAge time.Duration) {
	checkedAt := h.repo.CapabilitiesCheckedAt()
	if !checkedAt.IsZero() && time.Since(checkedAt) < maxAge {
		return
	}
	h.service.RefreshNATSCapabilities()
}
