package jetstream

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) jetStreamForRequest(w http.ResponseWriter, r *http.Request) (nats.JetStreamContext, func(), bool) {
	accountPublicKey := strings.TrimSpace(r.URL.Query().Get("accountPublicKey"))
	if accountPublicKey == "" {
		writeError(w, http.StatusBadRequest, "accountPublicKey query parameter is required")
		return nil, nil, false
	}
	account, ok := h.repo.FindAnyByPublicKey(accountPublicKey)
	if !ok {
		writeError(w, http.StatusNotFound, "account not found")
		return nil, nil, false
	}
	if !account.JSEnabled {
		writeError(w, http.StatusPreconditionFailed, "jetstream is disabled for this account")
		return nil, nil, false
	}
	signingKey, err := h.accountsSvc.EnsureAccountSigningKey(account.Operator, account.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to prepare account signing key")
		return nil, nil, false
	}
	nc, err := h.getOrCreateAccountConn(accountPublicKey, signingKey.Seed)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return nil, nil, false
	}
	js, err := nc.JetStream()
	if err != nil {
		writeError(w, http.StatusBadGateway, "jetstream unavailable for account")
		return nil, nil, false
	}
	return js, func() {}, true
}

func writeJetStreamError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, nats.ErrStreamNotFound) || errors.Is(err, nats.ErrConsumerNotFound) {
		writeError(w, http.StatusNotFound, fallback)
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func (h *Handler) getOrCreateAccountConn(accountPublicKey, signingKeySeed string) (*nats.Conn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if nc, ok := h.accountConns[accountPublicKey]; ok && nc.IsConnected() {
		return nc, nil
	}
	userJWT, userSeed, err := generateUserJWTForAccount(signingKeySeed, accountPublicKey)
	if err != nil {
		return nil, err
	}
	nc, err := nats.Connect(h.cfg.NATSURL, nats.Name("nats-console-api-js-account"), nats.Timeout(5*time.Second), nats.UserJWT(
		func() (string, error) { return userJWT, nil },
		func(nonce []byte) ([]byte, error) {
			kp, err := nkeys.FromSeed(userSeed)
			if err != nil {
				return nil, err
			}
			return kp.Sign(nonce)
		},
	))
	if err != nil {
		return nil, fmt.Errorf("failed to connect account jetstream: %w", err)
	}
	h.accountConns[accountPublicKey] = nc
	return nc, nil
}
