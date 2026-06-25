package jetstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	"github.com/iterating-io/nats-console/api/internal/accounts"
	"github.com/iterating-io/nats-console/api/internal/config"
)

type Handler struct {
	cfg          config.Config
	repo         *accounts.Repository
	accountsSvc  *accounts.Service
	accountConns map[string]*connGroup
	mu           sync.Mutex
}

func NewHandler(cfg config.Config, repo *accounts.Repository, accountsSvc *accounts.Service) *Handler {
	return &Handler{cfg: cfg, repo: repo, accountsSvc: accountsSvc, accountConns: make(map[string]*connGroup)}
}

type connGroup struct {
	ephemeral *nats.Conn
	users     map[string]*nats.Conn
}

func (h *Handler) ListStreams(w http.ResponseWriter, r *http.Request) {
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	names := []string{}
	for name := range js.StreamNames(nats.Context(ctx)) {
		names = append(names, name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"streams": names})
}

func (h *Handler) GetStream(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	info, err := js.StreamInfo(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "stream not found")
		return
	}
	subjects := info.Config.Subjects
	if subjects == nil {
		subjects = []string{}
	}
	// Return stream config and state so the UI can show the same details
	// as the `nats stream info` CLI command (config + state + cluster).
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     info.Config.Name,
		"subjects": subjects,
		"config":   info.Config,
		"state":    info.State,
		"cluster":  info.Cluster,
		"created":  info.Created,
	})
}

func (h *Handler) CreateStream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string   `json:"name"`
		Subjects []string `json:"subjects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	cfg := &nats.StreamConfig{Name: req.Name}
	if len(req.Subjects) > 0 {
		cfg.Subjects = req.Subjects
	} else {
		cfg.Subjects = []string{req.Name + ".>"}
	}
	_, err := js.AddStream(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "subjects": cfg.Subjects})
}

func (h *Handler) UpdateStream(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var req struct {
		Subjects []string `json:"subjects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	info, err := js.StreamInfo(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "stream not found")
		return
	}
	cfg := info.Config
	if len(req.Subjects) > 0 {
		cfg.Subjects = req.Subjects
	} else {
		cfg.Subjects = []string{}
	}
	_, err = js.UpdateStream(&cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "subjects": req.Subjects})
}

func (h *Handler) DeleteStream(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	if err := js.DeleteStream(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) PurgeStream(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	// Use a short-lived ephemeral user JWT that is allowed to publish only
	// to the purge subject for this stream. This keeps stored users like
	// `stream-reader` read-only and avoids persisting new credentials.
	accountPublicKey := strings.TrimSpace(r.URL.Query().Get("accountPublicKey"))
	if accountPublicKey == "" {
		writeError(w, http.StatusBadRequest, "accountPublicKey query parameter is required")
		return
	}
	account, ok := h.repo.FindAnyByPublicKey(accountPublicKey)
	if !ok {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	signingKey, err := h.accountsSvc.EnsureAccountSigningKey(account.Operator, account.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to prepare account signing key")
		return
	}

	purgeSubject := "$JS.API.STREAM.PURGE." + name
	userJWT, userSeed, err := generateEphemeralPurgeJWT(signingKey.Seed, accountPublicKey, purgeSubject, 1*time.Minute)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate ephemeral purge jwt")
		return
	}

	nc, err := nats.Connect(h.cfg.NATSURL, nats.Name("nats-console-api-js-purge"), nats.Timeout(10*time.Second), nats.UserJWT(
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
		writeError(w, http.StatusBadGateway, "failed to connect to nats for purge: "+err.Error())
		return
	}
	js, err := nc.JetStream()
	if err != nil {
		writeError(w, http.StatusBadGateway, "jetstream unavailable for account")
		nc.Close()
		return
	}
	if err := js.PurgeStream(name); err != nil {
		writeJetStreamError(w, err, "failed to purge stream")
		nc.Close()
		return
	}
	nc.Close()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListConsumers(w http.ResponseWriter, r *http.Request) {
	stream := strings.TrimSpace(r.PathValue("name"))
	if stream == "" {
		writeError(w, http.StatusBadRequest, "stream name is required")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	type consumerInfo struct {
		Name          string `json:"name"`
		FilterSubject string `json:"filterSubject"`
	}
	consumers := []consumerInfo{}
	for info := range js.ConsumersInfo(stream, nats.Context(ctx)) {
		consumers = append(consumers, consumerInfo{Name: info.Name, FilterSubject: info.Config.FilterSubject})
	}
	writeJSON(w, http.StatusOK, map[string]any{"consumers": consumers})
}

func (h *Handler) CreateConsumer(w http.ResponseWriter, r *http.Request) {
	stream := strings.TrimSpace(r.PathValue("name"))
	if stream == "" {
		writeError(w, http.StatusBadRequest, "stream name is required")
		return
	}
	var req struct {
		Name          string `json:"name"`
		FilterSubject string `json:"filterSubject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	_, err := js.AddConsumer(stream, &nats.ConsumerConfig{Durable: req.Name, FilterSubject: req.FilterSubject, AckPolicy: nats.AckExplicitPolicy})
	if err != nil {
		writeJetStreamError(w, err, "failed to create consumer")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "stream": stream})
}

func (h *Handler) DeleteConsumer(w http.ResponseWriter, r *http.Request) {
	stream := strings.TrimSpace(r.PathValue("name"))
	consumer := strings.TrimSpace(r.PathValue("consumer"))
	if stream == "" {
		writeError(w, http.StatusBadRequest, "stream name is required")
		return
	}
	if consumer == "" {
		writeError(w, http.StatusBadRequest, "consumer name is required")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	if err := js.DeleteConsumer(stream, consumer); err != nil {
		writeJetStreamError(w, err, "failed to delete consumer")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetLastMessage(w http.ResponseWriter, r *http.Request) {
	stream := strings.TrimSpace(r.PathValue("name"))
	if stream == "" {
		writeError(w, http.StatusBadRequest, "stream name is required")
		return
	}
	accountPublicKey := strings.TrimSpace(r.URL.Query().Get("accountPublicKey"))
	if accountPublicKey == "" {
		writeError(w, http.StatusBadRequest, "accountPublicKey query parameter is required")
		return
	}
	acc, ok := h.repo.FindAnyByPublicKey(accountPublicKey)
	if !ok {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err := h.accountsSvc.EnsureStreamReaderUser(acc.Operator, accountPublicKey); err != nil {
		writeError(w, http.StatusBadGateway, "failed to ensure stream reader user: "+err.Error())
		return
	}
	// Connect specifically as the stored `stream-reader` user so the GET message
	// API call uses that user's permissions. Do not replace the default
	// connection used by other handlers which require broader permissions.
	nc, err := h.getOrCreateAccountConnAsUser(acc.Operator, accountPublicKey, "stream-reader")
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to connect as stream-reader: "+err.Error())
		return
	}
	js, err := nc.JetStream()
	if err != nil {
		writeError(w, http.StatusBadGateway, "jetstream unavailable for account")
		return
	}
	info, err := js.StreamInfo(stream)
	if err != nil {
		writeJetStreamError(w, err, "stream not found")
		return
	}
	lastSeq := info.State.LastSeq
	if lastSeq == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"stream": stream, "seq": 0, "message": nil})
		return
	}
	msg, err := js.GetMsg(stream, lastSeq)
	if err != nil {
		writeJetStreamError(w, err, "failed to get message")
		return
	}
	payload := base64.StdEncoding.EncodeToString(msg.Data)
	resp := map[string]any{
		"stream":   stream,
		"seq":      lastSeq,
		"subject":  msg.Subject,
		"payload":  payload,
		"encoding": "base64",
	}
	writeJSON(w, http.StatusOK, resp)
}
