package accounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nats-io/nkeys"
)

const streamReaderUserName = "stream-reader"

var streamReaderPublishAllow = []string{
	"$JS.API.STREAM.INFO.*",
	"$JS.API.STREAM.MSG.GET.*",
}

var streamReaderPublishDeny = []string{
	"$JS.API.CONSUMER.>",
	"$JS.API.STREAM.CREATE.>",
	"$JS.API.STREAM.UPDATE.>",
	"$JS.API.STREAM.DELETE.>",
	"$JS.API.STREAM.PURGE.>",
	"$JS.API.STREAM.MSG.DELETE.>",
}

var streamReaderSubscribeAllow = []string{"_INBOX.>"}

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

func (h *Handler) ensureStreamReaderUser(acc Record) error {
	kp, err := nkeys.CreateUser()
	if err != nil {
		return fmt.Errorf("create user keypair: %w", err)
	}
	pubKey, err := kp.PublicKey()
	if err != nil {
		return fmt.Errorf("user public key: %w", err)
	}
	seed, err := kp.Seed()
	if err != nil {
		return fmt.Errorf("export user seed: %w", err)
	}
	if _, err := h.store.CreateUser(
		acc.Operator,
		acc.Name,
		acc.PublicKey,
		streamReaderUserName,
		pubKey,
		string(seed),
	); err != nil {
		return fmt.Errorf("persist user: %w", err)
	}
	for _, subject := range streamReaderPublishAllow {
		if _, err := h.store.AddUserPublishAllow(acc.Operator, acc.PublicKey, streamReaderUserName, subject); err != nil {
			return fmt.Errorf("persist publish allow %q: %w", subject, err)
		}
	}
	for _, subject := range streamReaderPublishDeny {
		if _, err := h.store.AddUserPublishDeny(acc.Operator, acc.PublicKey, streamReaderUserName, subject); err != nil {
			return fmt.Errorf("persist publish deny %q: %w", subject, err)
		}
	}
	for _, subject := range streamReaderSubscribeAllow {
		if _, err := h.store.AddUserSubscribeAllow(acc.Operator, acc.PublicKey, streamReaderUserName, subject); err != nil {
			return fmt.Errorf("persist subscribe allow %q: %w", subject, err)
		}
	}
	return nil
}
