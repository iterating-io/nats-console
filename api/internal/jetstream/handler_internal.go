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

func streamSources(names []string, streamName string) ([]*nats.StreamSource, error) {
	seen := make(map[string]struct{}, len(names))
	sources := make([]*nats.StreamSource, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if name == streamName {
			return nil, errors.New("a stream cannot be its own source")
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		sources = append(sources, &nats.StreamSource{Name: name})
	}
	return sources, nil
}

// appendStreamSource changes only the Sources field of an existing stream
// configuration. All other stream settings remain intact when the config is
// sent back to JetStream.
func appendStreamSource(sources []*nats.StreamSource, source *nats.StreamSource) ([]*nats.StreamSource, error) {
	for _, existing := range sources {
		if sameStreamSource(existing, source) {
			return nil, errors.New("stream source is already configured")
		}
	}
	return append(sources, source), nil
}

func streamSourceFilters(value string) []string {
	filters := []string{}
	seen := map[string]struct{}{}
	for _, filter := range strings.Split(value, ",") {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		if _, found := seen[filter]; found {
			continue
		}
		seen[filter] = struct{}{}
		filters = append(filters, filter)
	}
	return filters
}

func streamSourcesForFilters(source *nats.StreamSource, filters []string) []*nats.StreamSource {
	if len(filters) == 0 {
		filters = []string{""}
	}
	result := make([]*nats.StreamSource, 0, len(filters))
	for _, filter := range filters {
		copy := *source
		copy.FilterSubject = filter
		result = append(result, &copy)
	}
	return result
}

// updateStreamSourceFilter changes the filter on one configured source while
// retaining its account routing and every other source setting.
func updateStreamSourceFilters(sources []*nats.StreamSource, source *nats.StreamSource, currentFilter string, filters []string) ([]*nats.StreamSource, error) {
	updated := make([]*nats.StreamSource, 0, len(sources)+len(filters))
	var existingSource *nats.StreamSource
	for _, existing := range sources {
		if !sameStreamSourceIdentity(existing, source) || existing.FilterSubject != currentFilter {
			updated = append(updated, existing)
			continue
		}
		existingSource = existing
	}
	if existingSource == nil {
		return nil, errors.New("stream source is not configured")
	}
	for _, replacement := range streamSourcesForFilters(existingSource, filters) {
		for _, existing := range updated {
			if sameStreamSource(existing, replacement) {
				return nil, errors.New("stream source is already configured")
			}
		}
		updated = append(updated, replacement)
	}
	return updated, nil
}

func removeStreamSource(sources []*nats.StreamSource, source *nats.StreamSource) ([]*nats.StreamSource, error) {
	updated := make([]*nats.StreamSource, 0, len(sources))
	removed := false
	for _, existing := range sources {
		if sameStreamSourceIdentity(existing, source) {
			removed = true
			continue
		}
		updated = append(updated, existing)
	}
	if !removed {
		return nil, errors.New("stream source is not configured")
	}
	return updated, nil
}

func removeStreamSourceFilter(sources []*nats.StreamSource, source *nats.StreamSource, filter string) ([]*nats.StreamSource, error) {
	updated := make([]*nats.StreamSource, 0, len(sources)-1)
	removed := false
	for _, existing := range sources {
		if sameStreamSourceIdentity(existing, source) && existing.FilterSubject == filter && !removed {
			removed = true
			continue
		}
		updated = append(updated, existing)
	}
	if !removed {
		return nil, errors.New("stream source filter is not configured")
	}
	return updated, nil
}

func sameStreamSource(a, b *nats.StreamSource) bool {
	return sameStreamSourceIdentity(a, b) && a.FilterSubject == b.FilterSubject
}

func sameStreamSourceIdentity(a, b *nats.StreamSource) bool {
	if a == nil || b == nil || a.Name != b.Name {
		return false
	}
	if a.External == nil || b.External == nil {
		return a.External == nil && b.External == nil
	}
	return a.External.APIPrefix == b.External.APIPrefix && a.External.DeliverPrefix == b.External.DeliverPrefix
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
	nc, err := h.getOrCreateAccountConn(account.Operator, accountPublicKey, signingKey.Seed)
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

func (h *Handler) getOrCreateAccountConn(operator, accountPublicKey, signingKeySeed string) (*nats.Conn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cg := h.accountConns[accountPublicKey]
	if cg != nil && cg.ephemeral != nil && cg.ephemeral.IsConnected() {
		return cg.ephemeral, nil
	}
	// Default: create an ephemeral user JWT signed by the account signing key
	// and connect with that identity. This preserves the previous behavior
	// for most JetStream operations which may require broader permissions
	// than the restricted `stream-reader` user.
	userJWT, userSeed, err := generateUserJWTForAccount(signingKeySeed, accountPublicKey)
	if err != nil {
		return nil, err
	}

	// Try a few connect attempts with backoff to allow resolver updates to propagate.
	var nc *nats.Conn
	var connectErr error
	for i := 0; i < 3; i++ {
		nc, connectErr = nats.Connect(h.cfg.NATSURL, nats.Name("nats-console-api-js-account"), nats.Timeout(10*time.Second), nats.UserJWT(
			func() (string, error) { return userJWT, nil },
			func(nonce []byte) ([]byte, error) {
				kp, err := nkeys.FromSeed(userSeed)
				if err != nil {
					return nil, err
				}
				return kp.Sign(nonce)
			},
		))
		if connectErr == nil {
			break
		}
		if i == 2 {
			break
		}
		time.Sleep(time.Duration(250*(i+1)) * time.Millisecond)
	}
	if connectErr != nil {
		return nil, fmt.Errorf("failed to connect account jetstream: %w", connectErr)
	}
	cg = h.accountConns[accountPublicKey]
	if cg == nil {
		cg = &connGroup{users: make(map[string]*nats.Conn)}
	}
	cg.ephemeral = nc
	h.accountConns[accountPublicKey] = cg
	return nc, nil
}

func (h *Handler) resetEphemeralAccountConn(accountPublicKey string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cg := h.accountConns[accountPublicKey]
	if cg == nil || cg.ephemeral == nil {
		return
	}
	cg.ephemeral.Close()
	cg.ephemeral = nil
}

// getOrCreateAccountConnAsUser attempts to connect to NATS as a stored user
// (for example the "stream-reader" user). This is intended for read-only
// operations that require that user's specific permissions (e.g. GetMsg).
func (h *Handler) getOrCreateAccountConnAsUser(operator, accountPublicKey, userName string) (*nats.Conn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cg := h.accountConns[accountPublicKey]
	if cg != nil {
		if nc, ok := cg.users[userName]; ok && nc.IsConnected() {
			return nc, nil
		}
	}
	userJWT, userSeed, err := h.accountsSvc.BuildUserJWTForUser(operator, accountPublicKey, userName)
	if err != nil {
		return nil, err
	}
	// Try a few connect attempts with backoff.
	var nc *nats.Conn
	var connectErr error
	for i := 0; i < 3; i++ {
		nc, connectErr = nats.Connect(h.cfg.NATSURL, nats.Name("nats-console-api-js-account"), nats.Timeout(10*time.Second), nats.UserJWT(
			func() (string, error) { return userJWT, nil },
			func(nonce []byte) ([]byte, error) {
				kp, err := nkeys.FromSeed(userSeed)
				if err != nil {
					return nil, err
				}
				return kp.Sign(nonce)
			},
		))
		if connectErr == nil {
			break
		}
		if i == 2 {
			break
		}
		time.Sleep(time.Duration(250*(i+1)) * time.Millisecond)
	}
	if connectErr != nil {
		return nil, fmt.Errorf("failed to connect account jetstream as user %s: %w", userName, connectErr)
	}
	cg = h.accountConns[accountPublicKey]
	if cg == nil {
		cg = &connGroup{users: make(map[string]*nats.Conn)}
	}
	if cg.users == nil {
		cg.users = make(map[string]*nats.Conn)
	}
	cg.users[userName] = nc
	h.accountConns[accountPublicKey] = cg
	return nc, nil
}
