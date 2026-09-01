package jetstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
		Sources  []string `json:"sources"`
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
	sources, err := streamSources(req.Sources, req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	cfg.Sources = sources
	_, err = js.AddStream(cfg)
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

func (h *Handler) AddStreamSource(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	var req struct {
		SourceAccountPublicKey string `json:"sourceAccountPublicKey"`
		SourceName             string `json:"sourceName"`
		FilterSubject          string `json:"filterSubject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.SourceAccountPublicKey = strings.TrimSpace(req.SourceAccountPublicKey)
	req.SourceName = strings.TrimSpace(req.SourceName)
	req.FilterSubject = strings.TrimSpace(req.FilterSubject)
	if name == "" || req.SourceAccountPublicKey == "" || req.SourceName == "" {
		writeError(w, http.StatusBadRequest, "stream and source are required")
		return
	}
	targetAccount := strings.TrimSpace(r.URL.Query().Get("accountPublicKey"))
	if targetAccount == "" {
		writeError(w, http.StatusBadRequest, "accountPublicKey query parameter is required")
		return
	}
	if name == req.SourceName && targetAccount == req.SourceAccountPublicKey {
		writeError(w, http.StatusBadRequest, "a stream cannot be its own source")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	info, err := js.StreamInfo(name)
	if err != nil {
		writeJetStreamError(w, err, "stream not found")
		return
	}
	if info.State.Consumers > 0 {
		writeError(w, http.StatusConflict, "cannot add a source after consumers exist")
		return
	}
	source := &nats.StreamSource{Name: req.SourceName}
	if req.SourceAccountPublicKey != targetAccount {
		source.External = &nats.ExternalStream{APIPrefix: "$JS.SOURCE." + req.SourceAccountPublicKey + ".API", DeliverPrefix: "$JS.SOURCE." + targetAccount}
	}
	sources := info.Config.Sources
	for _, additional := range streamSourcesForFilters(source, streamSourceFilters(req.FilterSubject)) {
		var err error
		sources, err = appendStreamSource(sources, additional)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}
	if req.SourceAccountPublicKey != targetAccount {
		if err := h.accountsSvc.GrantJetStreamSource(req.SourceAccountPublicKey, targetAccount); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	info.Config.Sources = sources
	if _, err = js.UpdateStream(&info.Config); err != nil {
		writeJetStreamError(w, err, "failed to add stream source")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": source})
}

func (h *Handler) UpdateStreamSource(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	var req struct {
		SourceAccountPublicKey string `json:"sourceAccountPublicKey"`
		SourceName             string `json:"sourceName"`
		CurrentFilterSubject   string `json:"currentFilterSubject"`
		FilterSubject          string `json:"filterSubject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.SourceAccountPublicKey = strings.TrimSpace(req.SourceAccountPublicKey)
	req.SourceName = strings.TrimSpace(req.SourceName)
	req.CurrentFilterSubject = strings.TrimSpace(req.CurrentFilterSubject)
	req.FilterSubject = strings.TrimSpace(req.FilterSubject)
	if name == "" || req.SourceAccountPublicKey == "" || req.SourceName == "" {
		writeError(w, http.StatusBadRequest, "stream and source are required")
		return
	}
	targetAccount := strings.TrimSpace(r.URL.Query().Get("accountPublicKey"))
	if targetAccount == "" {
		writeError(w, http.StatusBadRequest, "accountPublicKey query parameter is required")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	info, err := js.StreamInfo(name)
	if err != nil {
		writeJetStreamError(w, err, "stream not found")
		return
	}
	if info.State.Consumers > 0 {
		writeError(w, http.StatusConflict, "cannot update a source after consumers exist")
		return
	}
	source := &nats.StreamSource{Name: req.SourceName}
	if req.SourceAccountPublicKey != targetAccount {
		source.External = &nats.ExternalStream{
			APIPrefix:     "$JS.SOURCE." + req.SourceAccountPublicKey + ".API",
			DeliverPrefix: "$JS.SOURCE." + targetAccount,
		}
	}
	sources, err := updateStreamSourceFilters(info.Config.Sources, source, req.CurrentFilterSubject, streamSourceFilters(req.FilterSubject))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	info.Config.Sources = sources
	if _, err = js.UpdateStream(&info.Config); err != nil {
		writeJetStreamError(w, err, "failed to update stream source")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (h *Handler) RemoveStreamSourceFilter(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	var req struct {
		SourceAccountPublicKey string  `json:"sourceAccountPublicKey"`
		SourceName             string  `json:"sourceName"`
		FilterSubject          *string `json:"filterSubject"`
		RemoveAll              bool    `json:"removeAll"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.SourceAccountPublicKey = strings.TrimSpace(req.SourceAccountPublicKey)
	req.SourceName = strings.TrimSpace(req.SourceName)
	filterSubject := ""
	if req.FilterSubject != nil {
		filterSubject = strings.TrimSpace(*req.FilterSubject)
	}
	if name == "" || req.SourceAccountPublicKey == "" || req.SourceName == "" {
		writeError(w, http.StatusBadRequest, "stream and source are required")
		return
	}
	if !req.RemoveAll && filterSubject == "" {
		writeError(w, http.StatusBadRequest, "filter is required unless removeAll is true")
		return
	}
	targetAccount := strings.TrimSpace(r.URL.Query().Get("accountPublicKey"))
	if targetAccount == "" {
		writeError(w, http.StatusBadRequest, "accountPublicKey query parameter is required")
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	info, err := js.StreamInfo(name)
	if err != nil {
		writeJetStreamError(w, err, "stream not found")
		return
	}
	if info.State.Consumers > 0 {
		writeError(w, http.StatusConflict, "cannot update a source after consumers exist")
		return
	}
	source := &nats.StreamSource{Name: req.SourceName}
	if req.SourceAccountPublicKey != targetAccount {
		source.External = &nats.ExternalStream{
			APIPrefix:     "$JS.SOURCE." + req.SourceAccountPublicKey + ".API",
			DeliverPrefix: "$JS.SOURCE." + targetAccount,
		}
	}
	var sources []*nats.StreamSource
	if req.RemoveAll {
		sources, err = removeStreamSource(info.Config.Sources, source)
	} else {
		sources, err = removeStreamSourceFilter(info.Config.Sources, source, filterSubject)
	}
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	info.Config.Sources = sources
	if _, err = js.UpdateStream(&info.Config); err != nil {
		message := "failed to remove stream source filter"
		if req.RemoveAll {
			message = "failed to remove stream source"
		}
		writeJetStreamError(w, err, message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (h *Handler) PublishMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subject string `json:"subject"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Subject = strings.TrimSpace(req.Subject)
	if req.Subject == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}
	accountPublicKey := strings.TrimSpace(r.URL.Query().Get("accountPublicKey"))
	if account, ok := h.repo.FindAnyByPublicKey(accountPublicKey); ok &&
		len(account.PublishAllow) > 0 &&
		!accounts.SubjectAllowed(req.Subject, account.PublishAllow) {
		writeError(w, http.StatusForbidden, "subject is not allowed for this account")
		return
	}
	// Account permission changes are applied through a new account JWT. Drop the
	// cached ephemeral connection so this publish authenticates with current
	// account permissions rather than credentials issued before the update.
	h.resetEphemeralAccountConn(accountPublicKey)
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	ack, err := js.Publish(req.Subject, []byte(req.Message))
	if err != nil {
		writeJetStreamError(w, err, "failed to publish message")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"stream": ack.Stream, "sequence": ack.Sequence})
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
		DeliverPolicy string `json:"deliverPolicy"`
		AckPolicy     string `json:"ackPolicy"`
		AckWait       string `json:"ackWait"`
		MaxDeliver    int    `json:"maxDeliver"`
		MaxAckPending int    `json:"maxAckPending"`
		Type          string `json:"type"`
	}
	consumers := []consumerInfo{}
	for info := range js.ConsumersInfo(stream, nats.Context(ctx)) {
		consumerType := "pull"
		if info.Config.DeliverSubject != "" {
			consumerType = "push"
		}
		consumers = append(consumers, consumerInfo{
			Name: info.Name, FilterSubject: info.Config.FilterSubject,
			DeliverPolicy: consumerDeliverPolicyName(info.Config.DeliverPolicy),
			AckPolicy:     info.Config.AckPolicy.String(), AckWait: info.Config.AckWait.String(),
			MaxDeliver: info.Config.MaxDeliver, MaxAckPending: info.Config.MaxAckPending,
			Type: consumerType,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"consumers": consumers})
}

func consumerDeliverPolicyName(policy nats.DeliverPolicy) string {
	switch policy {
	case nats.DeliverLastPolicy:
		return "last"
	case nats.DeliverNewPolicy:
		return "new"
	case nats.DeliverByStartSequencePolicy:
		return "start-sequence"
	case nats.DeliverByStartTimePolicy:
		return "start-time"
	case nats.DeliverLastPerSubjectPolicy:
		return "last-per-subject"
	default:
		return "all"
	}
}

func parseConsumerOperationalSettings(ackWaitValue string, maxDeliverValue, maxAckPendingValue int) (time.Duration, int, int, error) {
	ackWait := 30 * time.Second
	if value := strings.TrimSpace(ackWaitValue); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return 0, 0, 0, fmt.Errorf("ackWait must be a positive duration")
		}
		ackWait = parsed
	}
	maxDeliver := maxDeliverValue
	if maxDeliver == 0 {
		maxDeliver = -1
	}
	if maxDeliver < -1 {
		return 0, 0, 0, fmt.Errorf("maxDeliver must be -1 or a positive number")
	}
	maxAckPending := maxAckPendingValue
	if maxAckPending == 0 {
		maxAckPending = 1000
	}
	if maxAckPending < 1 {
		return 0, 0, 0, fmt.Errorf("maxAckPending must be positive")
	}
	return ackWait, maxDeliver, maxAckPending, nil
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
		DeliverPolicy string `json:"deliverPolicy"`
		AckWait       string `json:"ackWait"`
		MaxDeliver    int    `json:"maxDeliver"`
		MaxAckPending int    `json:"maxAckPending"`
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
	deliverPolicy := nats.DeliverAllPolicy
	switch strings.TrimSpace(req.DeliverPolicy) {
	case "", "all":
	case "new":
		deliverPolicy = nats.DeliverNewPolicy
	default:
		writeError(w, http.StatusBadRequest, "deliverPolicy must be all or new")
		return
	}
	ackWait, maxDeliver, maxAckPending, err := parseConsumerOperationalSettings(req.AckWait, req.MaxDeliver, req.MaxAckPending)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	_, err = js.AddConsumer(stream, &nats.ConsumerConfig{
		Durable: req.Name, FilterSubject: strings.TrimSpace(req.FilterSubject),
		AckPolicy: nats.AckExplicitPolicy, DeliverPolicy: deliverPolicy,
		AckWait: ackWait, MaxDeliver: maxDeliver, MaxAckPending: maxAckPending,
	})
	if err != nil {
		writeJetStreamError(w, err, "failed to create consumer")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "stream": stream})
}

func (h *Handler) UpdateConsumer(w http.ResponseWriter, r *http.Request) {
	stream := strings.TrimSpace(r.PathValue("name"))
	consumer := strings.TrimSpace(r.PathValue("consumer"))
	if stream == "" || consumer == "" {
		writeError(w, http.StatusBadRequest, "stream and consumer names are required")
		return
	}
	var req struct {
		FilterSubject string `json:"filterSubject"`
		AckWait       string `json:"ackWait"`
		MaxDeliver    int    `json:"maxDeliver"`
		MaxAckPending int    `json:"maxAckPending"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ackWait, maxDeliver, maxAckPending, err := parseConsumerOperationalSettings(req.AckWait, req.MaxDeliver, req.MaxAckPending)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	js, cleanup, ok := h.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	info, err := js.ConsumerInfo(stream, consumer)
	if err != nil {
		writeJetStreamError(w, err, "consumer not found")
		return
	}
	config := info.Config
	config.FilterSubject = strings.TrimSpace(req.FilterSubject)
	config.AckWait = ackWait
	config.MaxDeliver = maxDeliver
	config.MaxAckPending = maxAckPending
	if _, err := js.UpdateConsumer(stream, &config); err != nil {
		writeJetStreamError(w, err, "failed to update consumer")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": consumer, "stream": stream})
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
