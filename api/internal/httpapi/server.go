package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	"github.com/taek/nats-console/api/internal/auth"
	"github.com/taek/nats-console/api/internal/config"
	"github.com/taek/nats-console/api/internal/store"
)

type operatorRecord struct {
	Name string `json:"name"`
}

type accountRecord struct {
	Name           string   `json:"name"`
	Operator       string   `json:"operator"`
	PublishAllow   []string `json:"publishAllow"`
	SubscribeAllow []string `json:"subscribeAllow"`
	PublicKey      string   `json:"publicKey"`
	IsSystem       bool     `json:"isSystem"`
	JSEnabled      bool     `json:"jsEnabled"`
}

type Server struct {
	cfg                   config.Config
	jwtSvc                *auth.Service
	natsConn              *nats.Conn
	jsConn                *nats.Conn
	upgrader              websocket.Upgrader
	store                 *store.Store
	jsConnMu              sync.Mutex
	mu                    sync.RWMutex
	operators             []operatorRecord
	accounts              []accountRecord
	capabilities          serverCapabilities
	capabilitiesCheckedAt time.Time
	accountConns          map[string]*nats.Conn
	accountConnsMu        sync.Mutex
}

type serverCapabilities struct {
	AccountDelete bool `json:"accountDelete"`
}

type ctxKey string

const userClaimsKey ctxKey = "userClaims"

func New(cfg config.Config, jwtSvc *auth.Service, natsConn *nats.Conn, st *store.Store) *Server {
	return &Server{
		cfg:          cfg,
		jwtSvc:       jwtSvc,
		natsConn:     natsConn,
		store:        st,
		accountConns: make(map[string]*nats.Conn),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				allowed := strings.TrimSpace(cfg.AllowedOrigins)
				if allowed == "*" || allowed == "" {
					return true
				}
				return strings.EqualFold(r.Header.Get("Origin"), allowed)
			},
		},
	}
}

func (s *Server) SetNATSConn(nc *nats.Conn) {
	s.mu.Lock()
	s.natsConn = nc
	s.mu.Unlock()
}

func (s *Server) SetJetStreamConn(nc *nats.Conn) {
	s.mu.Lock()
	s.jsConn = nc
	s.mu.Unlock()
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("GET /api/v1/streams", s.withAuth(s.listStreams))
	mux.HandleFunc("POST /api/v1/streams", s.withAuth(s.createStream))
	mux.HandleFunc("GET /api/v1/streams/{name}", s.withAuth(s.getStream))
	mux.HandleFunc("PATCH /api/v1/streams/{name}", s.withAuth(s.updateStream))
	mux.HandleFunc("DELETE /api/v1/streams/{name}", s.withAuth(s.deleteStream))
	mux.HandleFunc("GET /api/v1/streams/{name}/consumers", s.withAuth(s.listConsumers))
	mux.HandleFunc("POST /api/v1/streams/{name}/consumers", s.withAuth(s.createConsumer))
	mux.HandleFunc("DELETE /api/v1/streams/{name}/consumers/{consumer}", s.withAuth(s.deleteConsumer))
	mux.HandleFunc("GET /api/v1/system/jetstream-status", s.withAuth(s.getJetStreamStatus))
	mux.HandleFunc("POST /api/v1/system/grant-jetstream", s.withAuth(s.grantJetStream))
	mux.HandleFunc("POST /api/v1/publish", s.withAuth(s.publish))
	mux.HandleFunc("GET /api/ws", s.ws)
	mux.HandleFunc("GET /api/v1/operators", s.withAuth(s.listOperators))
	mux.HandleFunc("GET /api/v1/accounts", s.withAuth(s.listAccounts))
	mux.HandleFunc("POST /api/v1/accounts", s.withAuth(s.createAccount))
	mux.HandleFunc("DELETE /api/v1/accounts/{operator}/{accountPublicKey}", s.withAuth(s.deleteAccount))
	mux.HandleFunc("GET /api/v1/accounts/{operator}/{accountPublicKey}/jwt", s.withAuth(s.getAccountJWT))
	mux.HandleFunc("POST /api/v1/accounts/{operator}/{accountPublicKey}/jetstream", s.withAuth(s.toggleAccountJetStream))
	mux.HandleFunc("POST /api/v1/accounts/{operator}/{name}/publish-allow", s.withAuth(s.addPublishAllow))
	mux.HandleFunc("DELETE /api/v1/accounts/{operator}/{name}/publish-allow", s.withAuth(s.removePublishAllow))
	mux.HandleFunc("POST /api/v1/accounts/{operator}/{name}/subscribe-allow", s.withAuth(s.addSubscribeAllow))
	mux.HandleFunc("DELETE /api/v1/accounts/{operator}/{name}/subscribe-allow", s.withAuth(s.removeSubscribeAllow))
	mux.HandleFunc("GET /api/v1/accounts/{operator}/{accountPublicKey}/users", s.withAuth(s.listUsers))
	mux.HandleFunc("POST /api/v1/accounts/{operator}/{accountPublicKey}/users", s.withAuth(s.createUser))
	mux.HandleFunc("DELETE /api/v1/accounts/{operator}/{accountPublicKey}/users/{user}", s.withAuth(s.deleteUser))
	mux.HandleFunc("GET /api/v1/accounts/{operator}/{accountPublicKey}/users/{user}/creds", s.withAuth(s.getUserCreds))
	mux.HandleFunc("POST /api/v1/accounts/{operator}/{accountPublicKey}/users/{user}/publish-allow", s.withAuth(s.addUserPublishAllow))
	mux.HandleFunc("DELETE /api/v1/accounts/{operator}/{accountPublicKey}/users/{user}/publish-allow", s.withAuth(s.removeUserPublishAllow))
	mux.HandleFunc("GET /api/v1/users", s.withAuth(s.listAllUsers))
	mux.HandleFunc("POST /api/v1/publish/as-user", s.withAuth(s.publishAsUser))
	return s.withCORS(mux)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.cfg.AllowedOrigins == "*" && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", s.cfg.AllowedOrigins)
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	natsState := "disconnected"
	if s.natsConn != nil && s.natsConn.IsConnected() {
		natsState = "connected"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"nats":     natsState,
		"serverTs": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Only admin account is supported; other roles are managed through NATS console UI
	if req.Username != s.cfg.AdminID || req.Password != s.cfg.AdminPassword {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := s.jwtSvc.Issue(req.Username, "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accessToken": token,
		"role":        "admin",
	})
}

func (s *Server) getJetStreamStatus(w http.ResponseWriter, r *http.Request) {
	if s.jsConn == nil || !s.jsConn.IsConnected() {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":        false,
			"grantSupported": true,
			"reason":         "jetstream app account not connected",
		})
		return
	}
	js, err := s.jsConn.JetStream()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":        false,
			"grantSupported": true,
			"reason":         err.Error(),
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	_, err = js.AccountInfo(nats.Context(ctx))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":        false,
			"grantSupported": true,
			"reason":         err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        true,
		"grantSupported": true,
	})
}

func (s *Server) grantJetStream(w http.ResponseWriter, _ *http.Request) {
	if err := s.EnsureJetStreamAccountAndConnect(); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) EnsureJetStreamAccountAndConnect() error {
	s.jsConnMu.Lock()
	defer s.jsConnMu.Unlock()

	jsAccountName := config.JSAccountName
	if s.cfg.OperatorNKey == "" {
		return fmt.Errorf("OPERATOR_NKEY not configured")
	}
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		return fmt.Errorf("nats is not connected")
	}

	operatorName := s.currentOperatorName()
	if operatorName == "" {
		operatorName = "default"
	}

	var target *accountRecord
	s.mu.RLock()
	for i := range s.accounts {
		acc := s.accounts[i]
		if acc.Operator == operatorName && strings.EqualFold(acc.Name, jsAccountName) {
			a := acc
			target = &a
			break
		}
	}
	s.mu.RUnlock()

	if target == nil {
		return fmt.Errorf("default JetStream account %q not found under operator %q", jsAccountName, operatorName)
	}

	claims, err := s.lookupAccountClaims(target.PublicKey)
	if err != nil {
		claims = natsjwt.NewAccountClaims(target.PublicKey)
	}
	claims.Name = jsAccountName
	claims.IssuedAt = time.Now().Unix()
	claims.Limits.JetStreamLimits.DiskStorage = -1
	claims.Limits.JetStreamLimits.MemoryStorage = -1
	claims.Limits.JetStreamLimits.Streams = -1
	claims.Limits.JetStreamLimits.Consumer = -1

	opKP, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return fmt.Errorf("invalid OPERATOR_NKEY")
	}
	if _, err := s.pushAccountClaimsToNATS(claims, opKP); err != nil {
		return fmt.Errorf("failed to update jetstream account claims: %w", err)
	}

	signingKey, err := s.ensureAccountSigningKey(operatorName, target.PublicKey)
	if err != nil {
		return fmt.Errorf("prepare jetstream account signing key: %w", err)
	}

	userJWT, userSeed, err := generateUserJWTForAccount(signingKey.Seed, target.PublicKey)
	if err != nil {
		return fmt.Errorf("generate jetstream user JWT: %w", err)
	}
	jsOpts := []nats.Option{
		nats.Name("nats-console-api-js"),
		nats.Timeout(5 * time.Second),
		nats.ReconnectWait(3 * time.Second),
		nats.MaxReconnects(-1),
		nats.RetryOnFailedConnect(true),
		nats.UserJWT(
			func() (string, error) { return userJWT, nil },
			func(nonce []byte) ([]byte, error) {
				kp, err := nkeys.FromSeed(userSeed)
				if err != nil {
					return nil, err
				}
				return kp.Sign(nonce)
			},
		),
	}

	jsConn, err := nats.Connect(s.cfg.NATSURL, jsOpts...)
	if err != nil {
		return fmt.Errorf("connect jetstream account: %w", err)
	}
	old := s.jsConn
	s.SetJetStreamConn(jsConn)
	if old != nil {
		old.Close()
	}
	log.Printf("EnsureJetStreamAccountAndConnect: jetstream account ready: %s/%s", operatorName, jsAccountName)
	return nil
}

func generateUserJWTForAccount(accountSigningSeed, accountPublicKey string) (userJWT string, userSeed []byte, err error) {
	accountKP, err := nkeys.FromSeed([]byte(accountSigningSeed))
	if err != nil {
		return "", nil, err
	}
	userKP, err := nkeys.CreateUser()
	if err != nil {
		return "", nil, err
	}
	userPub, err := userKP.PublicKey()
	if err != nil {
		return "", nil, err
	}
	claims := natsjwt.NewUserClaims(userPub)
	claims.Name = "console-api-js"
	signerPub, err := accountKP.PublicKey()
	if err != nil {
		return "", nil, err
	}
	if signerPub != accountPublicKey {
		claims.IssuerAccount = accountPublicKey
	}
	claims.IssuedAt = time.Now().Unix()
	userJWT, err = claims.Encode(accountKP)
	if err != nil {
		return "", nil, err
	}
	userSeed, err = userKP.Seed()
	if err != nil {
		return "", nil, err
	}
	return userJWT, userSeed, nil
}

func (s *Server) currentOperatorName() string {
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		return ""
	}
	pingMsg, err := s.natsConn.Request("$SYS.REQ.SERVER.PING", nil, 2*time.Second)
	if err != nil {
		return ""
	}
	var serverInfo struct {
		Server struct {
			Operator string `json:"operator"`
		} `json:"server"`
	}
	if err := json.Unmarshal(pingMsg.Data, &serverInfo); err != nil {
		return ""
	}
	return strings.TrimSpace(serverInfo.Server.Operator)
}

func (s *Server) jetStream(w http.ResponseWriter) (nats.JetStreamContext, bool) {
	if s.jsConn == nil || !s.jsConn.IsConnected() {
		writeError(w, http.StatusBadGateway, "jetstream account is not connected")
		return nil, false
	}
	js, err := s.jsConn.JetStream()
	if err != nil {
		writeError(w, http.StatusBadGateway, "jetstream unavailable")
		return nil, false
	}
	return js, true
}

func (s *Server) jetStreamForRequest(w http.ResponseWriter, r *http.Request) (nats.JetStreamContext, func(), bool) {
	accountPublicKey := strings.TrimSpace(r.URL.Query().Get("accountPublicKey"))
	if accountPublicKey == "" {
		js, ok := s.jetStream(w)
		return js, func() {}, ok
	}

	s.mu.RLock()
	var account *accountRecord
	for i := range s.accounts {
		if s.accounts[i].PublicKey == accountPublicKey {
			accCopy := s.accounts[i]
			account = &accCopy
			break
		}
	}
	s.mu.RUnlock()
	if account == nil {
		writeError(w, http.StatusNotFound, "account not found")
		return nil, nil, false
	}
	if !account.JSEnabled {
		writeError(w, http.StatusPreconditionFailed, "jetstream is disabled for this account")
		return nil, nil, false
	}

	signingKey, err := s.ensureAccountSigningKey(account.Operator, account.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to prepare account signing key")
		return nil, nil, false
	}

	nc, err := s.getOrCreateAccountConn(accountPublicKey, signingKey.Seed)
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

func (s *Server) getOrCreateAccountConn(accountPublicKey, signingKeySeed string) (*nats.Conn, error) {
	s.accountConnsMu.Lock()
	defer s.accountConnsMu.Unlock()

	if nc, ok := s.accountConns[accountPublicKey]; ok && nc.IsConnected() {
		return nc, nil
	}

	userJWT, userSeed, err := generateUserJWTForAccount(signingKeySeed, accountPublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create account jetstream user: %w", err)
	}

	nc, err := nats.Connect(
		s.cfg.NATSURL,
		nats.Name("nats-console-api-js-account"),
		nats.Timeout(5*time.Second),
		nats.UserJWT(
			func() (string, error) { return userJWT, nil },
			func(nonce []byte) ([]byte, error) {
				kp, err := nkeys.FromSeed(userSeed)
				if err != nil {
					return nil, err
				}
				return kp.Sign(nonce)
			},
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect account jetstream: %w", err)
	}

	s.accountConns[accountPublicKey] = nc
	return nc, nil
}

func (s *Server) listStreams(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.URL.Query().Get("accountPublicKey")) == "" && (s.jsConn == nil || !s.jsConn.IsConnected()) {
		writeJSON(w, http.StatusOK, map[string]any{"streams": []string{}})
		return
	}
	js, cleanup, ok := s.jetStreamForRequest(w, r)
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

func (s *Server) getStream(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	js, cleanup, ok := s.jetStreamForRequest(w, r)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     info.Config.Name,
		"subjects": subjects,
	})
}

func (s *Server) createStream(w http.ResponseWriter, r *http.Request) {
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
	js, cleanup, ok := s.jetStreamForRequest(w, r)
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

func (s *Server) updateStream(w http.ResponseWriter, r *http.Request) {
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
	js, cleanup, ok := s.jetStreamForRequest(w, r)
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

func (s *Server) deleteStream(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	js, cleanup, ok := s.jetStreamForRequest(w, r)
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

func (s *Server) listConsumers(w http.ResponseWriter, r *http.Request) {
	stream := strings.TrimSpace(r.PathValue("name"))
	if stream == "" {
		writeError(w, http.StatusBadRequest, "stream name is required")
		return
	}
	js, cleanup, ok := s.jetStreamForRequest(w, r)
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
		consumers = append(consumers, consumerInfo{
			Name:          info.Name,
			FilterSubject: info.Config.FilterSubject,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"consumers": consumers})
}

func (s *Server) createConsumer(w http.ResponseWriter, r *http.Request) {
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
	js, cleanup, ok := s.jetStreamForRequest(w, r)
	if !ok {
		return
	}
	defer cleanup()
	_, err := js.AddConsumer(stream, &nats.ConsumerConfig{
		Durable:       req.Name,
		FilterSubject: req.FilterSubject,
		AckPolicy:     nats.AckExplicitPolicy,
	})
	if err != nil {
		writeJetStreamError(w, err, "failed to create consumer")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "stream": stream})
}

func (s *Server) deleteConsumer(w http.ResponseWriter, r *http.Request) {
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
	js, cleanup, ok := s.jetStreamForRequest(w, r)
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

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(userClaimsKey).(*auth.Claims)
	if !ok || (claims.Role != "admin" && claims.Role != "operator") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var req struct {
		Subject string `json:"subject"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Subject) == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		writeError(w, http.StatusBadGateway, "nats is not connected")
		return
	}
	if err := s.natsConn.Publish(req.Subject, []byte(req.Message)); err != nil {
		writeError(w, http.StatusBadGateway, "failed to publish")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"published": true,
		"subject":   req.Subject,
		"by":        claims.Username,
	})
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	tokenString := strings.TrimSpace(r.URL.Query().Get("token"))
	if tokenString == "" {
		writeError(w, http.StatusUnauthorized, "missing token")
		return
	}
	claims, err := s.jwtSvc.Parse(tokenString)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	defer conn.Close()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	if err := conn.WriteJSON(map[string]any{
		"type":       "session",
		"username":   claims.Username,
		"role":       claims.Role,
		"serverTime": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return
	}
	for range ticker.C {
		natsState := "disconnected"
		if s.natsConn != nil && s.natsConn.IsConnected() {
			natsState = "connected"
		}
		if err := conn.WriteJSON(map[string]any{
			"type":       "heartbeat",
			"nats":       natsState,
			"serverTime": time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			return
		}
	}
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
		claims, err := s.jwtSvc.Parse(token)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidToken) {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			writeError(w, http.StatusUnauthorized, "token validation failed")
			return
		}
		ctx := context.WithValue(r.Context(), userClaimsKey, claims)
		next(w, r.WithContext(ctx))
	}
}

// ── Operator handlers (read-only, in-memory from NATS) ────────────────────────

func (s *Server) listOperators(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := s.operators
	if list == nil {
		list = []operatorRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"operators": list})
}

// ── Account handlers (in-memory) ──────────────────────────────────────────────

func (s *Server) listAccounts(w http.ResponseWriter, _ *http.Request) {
	s.refreshNATSCapabilitiesIfStale(2 * time.Second)
	s.mu.RLock()
	defer s.mu.RUnlock()
	jsAccountName := strings.TrimSpace(config.JSAccountName)
	list := make([]accountRecord, 0, len(s.accounts))
	for _, acc := range s.accounts {
		if !acc.IsSystem && !strings.EqualFold(acc.Name, jsAccountName) {
			list = append(list, acc)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts":     list,
		"capabilities": s.capabilities,
	})
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string   `json:"name"`
		Operator       string   `json:"operator"`
		PublishAllow   []string `json:"publishAllow"`
		SubscribeAllow []string `json:"subscribeAllow"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Operator = strings.TrimSpace(req.Operator)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Operator == "" {
		writeError(w, http.StatusBadRequest, "operator is required")
		return
	}
	s.mu.Lock()
	operatorExists := false
	for _, op := range s.operators {
		if op.Name == req.Operator {
			operatorExists = true
			break
		}
	}
	if !operatorExists {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "operator not found")
		return
	}
	for _, acc := range s.accounts {
		if acc.Operator == req.Operator && strings.EqualFold(acc.Name, req.Name) {
			s.mu.Unlock()
			writeError(w, http.StatusConflict, "account already exists in this operator")
			return
		}
	}
	accountKP, err := nkeys.CreateAccount()
	if err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "failed to create account key")
		return
	}
	pubKey, err := accountKP.PublicKey()
	if err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "failed to create account key")
		return
	}
	seed, err := accountKP.Seed()
	if err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "failed to create account key")
		return
	}
	if err := s.store.SaveAccountSigningKey(req.Operator, req.Name, pubKey, string(seed)); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "failed to persist account signing key")
		return
	}
	record := accountRecord{
		Name:           req.Name,
		Operator:       req.Operator,
		PublishAllow:   uniqueTrimmedSubjects(req.PublishAllow),
		SubscribeAllow: uniqueTrimmedSubjects(req.SubscribeAllow),
		PublicKey:      pubKey,
		IsSystem:       false,
	}
	s.accounts = append(s.accounts, record)
	s.mu.Unlock()

	if err := s.pushAccountToNATS(record); err != nil {
		s.mu.Lock()
		for i, acc := range s.accounts {
			if acc.Operator == record.Operator && acc.Name == record.Name {
				s.accounts = append(s.accounts[:i], s.accounts[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		if cleanupErr := s.store.DeleteAccountSigningKey(req.Operator, pubKey); cleanupErr != nil {
			log.Printf("createAccount: cleanup signing key for %s/%s: %v", req.Operator, req.Name, cleanupErr)
		}
		log.Printf("createAccount: push JWT for %s/%s: %v", req.Operator, req.Name, err)
		writeError(w, http.StatusBadGateway, "failed to update nats resolver")
		return
	}

	writeJSON(w, http.StatusCreated, record)
}

// pushAccountToNATS signs an account JWT with the operator NKey and pushes it to the NATS resolver.
func (s *Server) pushAccountToNATS(acc accountRecord) error {
	if s.cfg.OperatorNKey == "" {
		return fmt.Errorf("OPERATOR_NKEY not configured")
	}
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		return fmt.Errorf("NATS not connected")
	}
	opKP, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return fmt.Errorf("invalid operator nkey: %w", err)
	}
	pub := strings.TrimSpace(acc.PublicKey)
	if pub == "" {
		return fmt.Errorf("account public key is empty")
	}
	claims, err := s.lookupAccountClaims(pub)
	if err != nil {
		claims = natsjwt.NewAccountClaims(pub)
	}
	claims.Name = acc.Name
	claims.IssuedAt = time.Now().Unix()
	claims.Account.DefaultPermissions.Pub.Allow = append(
		[]string{},
		acc.PublishAllow...,
	)
	claims.Account.DefaultPermissions.Sub.Allow = append(
		[]string{},
		acc.SubscribeAllow...,
	)
	if sigKey, sigErr := s.store.GetAccountSigningKey(acc.Operator, pub); sigErr == nil {
		if sigKP, sigErr := nkeys.FromSeed([]byte(sigKey.Seed)); sigErr == nil {
			if sigPub, sigErr := sigKP.PublicKey(); sigErr == nil && sigPub != pub {
				if claims.Account.SigningKeys == nil {
					claims.Account.SigningKeys = make(natsjwt.SigningKeys)
				}
				claims.Account.SigningKeys.Add(sigPub)
			}
		}
	}
	msg, err := s.pushAccountClaimsToNATS(claims, opKP)
	if err != nil {
		return err
	}
	_ = msg
	return nil
}

func (s *Server) getAccountJWT(w http.ResponseWriter, r *http.Request) {
	operator := r.PathValue("operator")
	accountPublicKey := r.PathValue("accountPublicKey")
	if operator == "" || accountPublicKey == "" {
		writeError(w, http.StatusBadRequest, "operator and accountPublicKey are required")
		return
	}
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		writeError(w, http.StatusServiceUnavailable, "NATS not connected")
		return
	}
	lookupMsg, err := s.natsConn.Request(
		"$SYS.REQ.ACCOUNT."+accountPublicKey+".CLAIMS.LOOKUP",
		nil,
		2*time.Second,
	)
	if err != nil {
		writeError(w, http.StatusNotFound, "account JWT not found")
		return
	}
	rawJWT := string(lookupMsg.Data)
	claims, err := natsjwt.DecodeAccountClaims(rawJWT)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode account JWT")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jwt":     rawJWT,
		"payload": claims,
	})
}

func (s *Server) toggleAccountJetStream(w http.ResponseWriter, r *http.Request) {
	operator := r.PathValue("operator")
	accountPublicKey := r.PathValue("accountPublicKey")
	if operator == "" || accountPublicKey == "" {
		writeError(w, http.StatusBadRequest, "operator and accountPublicKey are required")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if s.cfg.OperatorNKey == "" {
		writeError(w, http.StatusBadRequest, "OPERATOR_NKEY not configured")
		return
	}
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		writeError(w, http.StatusBadGateway, "NATS not connected")
		return
	}

	claims, err := s.lookupAccountClaims(accountPublicKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "account JWT not found")
		return
	}

	if req.Enabled {
		claims.Limits.JetStreamLimits.DiskStorage = -1
		claims.Limits.JetStreamLimits.MemoryStorage = -1
		claims.Limits.JetStreamLimits.Streams = -1
		claims.Limits.JetStreamLimits.Consumer = -1
	} else {
		claims.Limits.JetStreamLimits = natsjwt.JetStreamLimits{}
	}
	claims.IssuedAt = time.Now().Unix()

	opKP, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid OPERATOR_NKEY")
		return
	}
	if _, err := s.pushAccountClaimsToNATS(claims, opKP); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	s.mu.Lock()
	for i, acc := range s.accounts {
		if acc.PublicKey == accountPublicKey && acc.Operator == operator {
			s.accounts[i].JSEnabled = req.Enabled
			break
		}
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": req.Enabled,
		"message": "JetStream updated for account",
	})
}

func (s *Server) lookupAccountClaims(accountPublicKey string) (*natsjwt.AccountClaims, error) {
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		return nil, fmt.Errorf("NATS not connected")
	}
	lookupMsg, err := s.natsConn.Request(
		"$SYS.REQ.ACCOUNT."+accountPublicKey+".CLAIMS.LOOKUP",
		nil,
		2*time.Second,
	)
	if err != nil {
		return nil, fmt.Errorf("lookup account claims: %w", err)
	}
	claims, err := natsjwt.DecodeAccountClaims(string(lookupMsg.Data))
	if err != nil {
		return nil, fmt.Errorf("decode account claims: %w", err)
	}
	return claims, nil
}

func (s *Server) pushAccountClaimsToNATS(claims *natsjwt.AccountClaims, opKP nkeys.KeyPair) (*nats.Msg, error) {
	jwt, err := claims.Encode(opKP)
	if err != nil {
		return nil, fmt.Errorf("encode account JWT: %w", err)
	}
	msg, err := s.natsConn.Request("$SYS.REQ.CLAIMS.UPDATE", []byte(jwt), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("push account JWT: %w", err)
	}
	var resp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(msg.Data, &resp) == nil && resp.Error != "" {
		return nil, fmt.Errorf("NATS rejected JWT: %s", resp.Error)
	}
	return msg, nil
}

func (s *Server) revokeUserInNATS(accountPublicKey, userPublicKey string) error {
	if s.cfg.OperatorNKey == "" {
		return fmt.Errorf("OPERATOR_NKEY not configured")
	}
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		return fmt.Errorf("NATS not connected")
	}
	opKP, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return fmt.Errorf("invalid operator nkey: %w", err)
	}
	claims, err := s.lookupAccountClaims(accountPublicKey)
	if err != nil {
		return err
	}
	claims.Revoke(strings.TrimSpace(userPublicKey))
	claims.IssuedAt = time.Now().Unix()
	_, err = s.pushAccountClaimsToNATS(claims, opKP)
	return err
}

func (s *Server) deleteAccountInNATS(accountPublicKey string) error {
	if s.cfg.OperatorNKey == "" {
		return fmt.Errorf("OPERATOR_NKEY not configured")
	}
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		return fmt.Errorf("NATS not connected")
	}
	opKP, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return fmt.Errorf("invalid operator nkey: %w", err)
	}
	opPub, err := opKP.PublicKey()
	if err != nil {
		return fmt.Errorf("operator public key: %w", err)
	}
	claims := natsjwt.NewGenericClaims(opPub)
	claims.Data["accounts"] = []string{strings.TrimSpace(accountPublicKey)}
	j, err := claims.Encode(opKP)
	if err != nil {
		return fmt.Errorf("encode account delete claim: %w", err)
	}
	msg, err := s.natsConn.Request("$SYS.REQ.CLAIMS.DELETE", []byte(j), 5*time.Second)
	if err != nil {
		return fmt.Errorf("push account delete claim: %w", err)
	}
	var resp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(msg.Data, &resp) == nil && resp.Error != "" {
		return fmt.Errorf("NATS rejected account delete: %s", resp.Error)
	}
	return nil
}

func (s *Server) detectAccountDeleteSupport() (bool, error) {
	if s.cfg.OperatorNKey == "" {
		return false, fmt.Errorf("OPERATOR_NKEY not configured")
	}
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		return false, fmt.Errorf("NATS not connected")
	}
	opKP, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return false, fmt.Errorf("invalid operator nkey: %w", err)
	}
	opPub, err := opKP.PublicKey()
	if err != nil {
		return false, fmt.Errorf("operator public key: %w", err)
	}
	probeKP, err := nkeys.CreateAccount()
	if err != nil {
		return false, fmt.Errorf("create probe account key: %w", err)
	}
	probePub, err := probeKP.PublicKey()
	if err != nil {
		return false, fmt.Errorf("probe account public key: %w", err)
	}
	claims := natsjwt.NewGenericClaims(opPub)
	claims.Data["accounts"] = []string{probePub}
	j, err := claims.Encode(opKP)
	if err != nil {
		return false, fmt.Errorf("encode delete probe claim: %w", err)
	}
	msg, err := s.natsConn.Request("$SYS.REQ.CLAIMS.DELETE", []byte(j), 5*time.Second)
	if err != nil {
		return false, fmt.Errorf("send delete probe claim: %w", err)
	}
	raw := strings.ToLower(string(msg.Data))
	if strings.Contains(raw, "delete must be enabled") {
		return false, nil
	}
	if strings.Contains(raw, "not found") || strings.Contains(raw, "missing") {
		return true, nil
	}
	var resp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(msg.Data, &resp) == nil && resp.Error != "" {
		normalized := strings.ToLower(resp.Error)
		if strings.Contains(normalized, "delete must be enabled") {
			return false, nil
		}
		if strings.Contains(normalized, "not found") || strings.Contains(normalized, "missing") {
			return true, nil
		}
		return false, fmt.Errorf("delete probe rejected: %s", resp.Error)
	}
	return true, nil
}

func (s *Server) RefreshNATSCapabilities() {
	accountDelete, err := s.detectAccountDeleteSupport()
	if err != nil {
		log.Printf("RefreshNATSCapabilities: account delete probe failed: %v", err)
	}
	s.mu.Lock()
	s.capabilities.AccountDelete = accountDelete
	s.capabilitiesCheckedAt = time.Now()
	s.mu.Unlock()
}

func (s *Server) refreshNATSCapabilitiesIfStale(maxAge time.Duration) {
	s.mu.RLock()
	checkedAt := s.capabilitiesCheckedAt
	s.mu.RUnlock()
	if !checkedAt.IsZero() && time.Since(checkedAt) < maxAge {
		return
	}
	s.RefreshNATSCapabilities()
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	if operator == "" || accountPublicKey == "" {
		writeError(w, http.StatusBadRequest, "operator and accountPublicKey are required")
		return
	}
	if strings.EqualFold(accountPublicKey, s.systemAccountPublicKey()) {
		writeError(w, http.StatusForbidden, "system account cannot be deleted")
		return
	}
	if s.isJetStreamManagedAccount(operator, accountPublicKey) {
		writeError(w, http.StatusForbidden, "jetstream managed account cannot be deleted")
		return
	}

	s.refreshNATSCapabilitiesIfStale(2 * time.Second)

	s.mu.RLock()
	accountDeleteEnabled := s.capabilities.AccountDelete
	s.mu.RUnlock()
	if !accountDeleteEnabled {
		writeError(w, http.StatusPreconditionFailed, "account delete is disabled in NATS resolver configuration")
		return
	}

	s.mu.RLock()
	accountExists := false
	for _, acc := range s.accounts {
		if acc.Operator == operator && acc.PublicKey == accountPublicKey {
			accountExists = true
			break
		}
	}
	s.mu.RUnlock()
	if !accountExists {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	if err := s.deleteAccountInNATS(accountPublicKey); err != nil {
		log.Printf("deleteAccount: failed for %s/%s: %v", operator, accountPublicKey, err)
		writeError(w, http.StatusBadGateway, "failed to delete account in nats")
		return
	}

	s.mu.Lock()
	filtered := s.accounts[:0]
	for _, acc := range s.accounts {
		if !(acc.Operator == operator && acc.PublicKey == accountPublicKey) {
			filtered = append(filtered, acc)
		}
	}
	s.accounts = filtered
	s.mu.Unlock()

	if err := s.store.DeleteAccountData(operator, accountPublicKey); err != nil {
		log.Printf("deleteAccount: local cleanup failed for %s/%s: %v", operator, accountPublicKey, err)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) isJetStreamManagedAccount(operator, accountPublicKey string) bool {
	jsAccountName := strings.TrimSpace(config.JSAccountName)
	if jsAccountName == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, acc := range s.accounts {
		if acc.Operator == operator && acc.PublicKey == accountPublicKey {
			return strings.EqualFold(acc.Name, jsAccountName)
		}
	}
	return false
}

func (s *Server) addPublishAllow(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	name := strings.TrimSpace(r.PathValue("name"))
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
	var updated accountRecord
	found := false
	s.mu.Lock()
	for i, acc := range s.accounts {
		if acc.Operator == operator && acc.Name == name {
			for _, sub := range acc.PublishAllow {
				if sub == subject {
					s.mu.Unlock()
					writeError(w, http.StatusConflict, "subject already exists")
					return
				}
			}
			s.accounts[i].PublishAllow = append(s.accounts[i].PublishAllow, subject)
			updated = s.accounts[i]
			found = true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err := s.pushAccountToNATS(updated); err != nil {
		writeError(w, http.StatusBadGateway, "failed to update nats resolver")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) removePublishAllow(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	name := strings.TrimSpace(r.PathValue("name"))
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
	var updated accountRecord
	found := false
	s.mu.Lock()
	for i, acc := range s.accounts {
		if acc.Operator == operator && acc.Name == name {
			filtered := acc.PublishAllow[:0]
			for _, sub := range acc.PublishAllow {
				if sub != subject {
					filtered = append(filtered, sub)
				}
			}
			s.accounts[i].PublishAllow = filtered
			updated = s.accounts[i]
			found = true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err := s.pushAccountToNATS(updated); err != nil {
		writeError(w, http.StatusBadGateway, "failed to update nats resolver")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) addSubscribeAllow(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	name := strings.TrimSpace(r.PathValue("name"))
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
	var updated accountRecord
	found := false
	s.mu.Lock()
	for i, acc := range s.accounts {
		if acc.Operator == operator && acc.Name == name {
			for _, sub := range acc.SubscribeAllow {
				if sub == subject {
					s.mu.Unlock()
					writeError(w, http.StatusConflict, "subject already exists")
					return
				}
			}
			s.accounts[i].SubscribeAllow = append(s.accounts[i].SubscribeAllow, subject)
			updated = s.accounts[i]
			found = true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err := s.pushAccountToNATS(updated); err != nil {
		writeError(w, http.StatusBadGateway, "failed to update nats resolver")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) removeSubscribeAllow(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	name := strings.TrimSpace(r.PathValue("name"))
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
	var updated accountRecord
	found := false
	s.mu.Lock()
	for i, acc := range s.accounts {
		if acc.Operator == operator && acc.Name == name {
			filtered := acc.SubscribeAllow[:0]
			for _, sub := range acc.SubscribeAllow {
				if sub != subject {
					filtered = append(filtered, sub)
				}
			}
			s.accounts[i].SubscribeAllow = filtered
			updated = s.accounts[i]
			found = true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err := s.pushAccountToNATS(updated); err != nil {
		writeError(w, http.StatusBadGateway, "failed to update nats resolver")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// ── User handlers (SQLite-backed) ─────────────────────────────────────────────

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	users, err := s.store.ListUsers(operator, accountPublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	var req struct {
		Name string `json:"name"`
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
	s.mu.RLock()
	var account *accountRecord
	for _, acc := range s.accounts {
		if acc.Operator == operator && acc.PublicKey == accountPublicKey {
			accCopy := acc
			account = &accCopy
			break
		}
	}
	s.mu.RUnlock()
	if account == nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	kp, err := nkeys.CreateUser()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate keypair")
		return
	}
	pubKey, err := kp.PublicKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get public key")
		return
	}
	seed, err := kp.Seed()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export user seed")
		return
	}
	record, err := s.store.CreateUser(operator, account.Name, account.PublicKey, req.Name, pubKey, string(seed))
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "user already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	user := strings.TrimSpace(r.PathValue("user"))
	u, err := s.store.GetUser(operator, accountPublicKey, user)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if err := s.revokeUserInNATS(accountPublicKey, u.PublicKey); err != nil {
		log.Printf("deleteUser: revoke failed for %s/%s/%s (%s): %v", operator, accountPublicKey, user, u.PublicKey, err)
		writeError(w, http.StatusBadGateway, "failed to revoke user in nats")
		return
	}
	if err := s.store.DeleteUser(operator, accountPublicKey, user); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addUserPublishAllow(w http.ResponseWriter, r *http.Request) {
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
	u, err := s.store.AddUserPublishAllow(operator, accountPublicKey, user, subject)
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

func (s *Server) removeUserPublishAllow(w http.ResponseWriter, r *http.Request) {
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
	u, err := s.store.RemoveUserPublishAllow(operator, accountPublicKey, user, subject)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) listAllUsers(w http.ResponseWriter, _ *http.Request) {
	list, err := s.store.ListAllUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": list})
}

func (s *Server) publishAsUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(userClaimsKey).(*auth.Claims)
	if !ok || (claims.Role != "admin" && claims.Role != "operator") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var req struct {
		Operator string `json:"operator"`
		Account  string `json:"account"`
		User     string `json:"user"`
		Subject  string `json:"subject"`
		Message  string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Operator = strings.TrimSpace(req.Operator)
	req.Account = strings.TrimSpace(req.Account)
	req.User = strings.TrimSpace(req.User)
	req.Subject = strings.TrimSpace(req.Subject)
	if req.Operator == "" || req.Account == "" || req.User == "" {
		writeError(w, http.StatusBadRequest, "operator, account, user are required")
		return
	}
	if req.Subject == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		writeError(w, http.StatusBadGateway, "nats is not connected")
		return
	}
	s.mu.RLock()
	var acc *accountRecord
	for i := range s.accounts {
		if s.accounts[i].Operator == req.Operator && s.accounts[i].Name == req.Account {
			acc = &s.accounts[i]
			break
		}
	}
	s.mu.RUnlock()
	if acc == nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	usr, err := s.store.GetUser(req.Operator, acc.PublicKey, req.User)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	accountAllowed := len(acc.PublishAllow) == 0 || subjectAllowed(req.Subject, acc.PublishAllow)
	userAllowed := len(usr.PublishAllow) == 0 || subjectAllowed(req.Subject, usr.PublishAllow)
	if !accountAllowed || !userAllowed {
		writeError(w, http.StatusForbidden, "subject is not allowed for this user")
		return
	}
	if err := s.natsConn.Publish(req.Subject, []byte(req.Message)); err != nil {
		writeError(w, http.StatusBadGateway, "failed to publish")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"published": true,
		"subject":   req.Subject,
		"by":        claims.Username,
		"asUser":    req.User,
		"account":   req.Account,
		"operator":  req.Operator,
	})
}

func subjectAllowed(subject string, allowed []string) bool {
	for _, rule := range allowed {
		if matchSubject(rule, subject) {
			return true
		}
	}
	return false
}

func uniqueTrimmedSubjects(subjects []string) []string {
	result := make([]string, 0, len(subjects))
	seen := make(map[string]struct{}, len(subjects))
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		if _, ok := seen[subject]; ok {
			continue
		}
		seen[subject] = struct{}{}
		result = append(result, subject)
	}
	return result
}

func matchSubject(rule, subject string) bool {
	rule = strings.TrimSpace(rule)
	subject = strings.TrimSpace(subject)
	if rule == "" || subject == "" {
		return false
	}
	rt := strings.Split(rule, ".")
	st := strings.Split(subject, ".")
	for i := 0; i < len(rt); i++ {
		if i >= len(st) {
			return rt[i] == ">" && i == len(rt)-1
		}
		switch rt[i] {
		case ">":
			return i == len(rt)-1
		case "*":
			continue
		default:
			if rt[i] != st[i] {
				return false
			}
		}
	}
	return len(st) == len(rt)
}

func (s *Server) getUserCreds(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	userName := strings.TrimSpace(r.PathValue("user"))

	userSeed, err := s.store.GetUserSeed(operator, accountPublicKey, userName)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if userSeed == "" {
		writeError(w, http.StatusUnprocessableEntity, "creds unavailable: this user was created before seed storage was enabled; delete and recreate the user")
		return
	}

	user, err := s.store.GetUser(operator, accountPublicKey, userName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	signingKey, err := s.ensureAccountSigningKey(operator, accountPublicKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to prepare account signing key: "+err.Error())
		return
	}

	creds, err := buildUserCreds(user.Name, user.PublicKey, userSeed, signingKey.Seed, accountPublicKey)
	if err != nil {
		log.Printf("getUserCreds: %s/%s/%s: %v", operator, accountPublicKey, userName, err)
		writeError(w, http.StatusInternalServerError, "failed to generate creds")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"creds": creds})
}

func (s *Server) ensureAccountSigningKey(operator, accountPublicKey string) (*store.AccountSigningKey, error) {
	key, err := s.store.GetAccountSigningKey(operator, accountPublicKey)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	// No signing key stored. Generate a dedicated one and push an updated account JWT.
	s.mu.RLock()
	var acc *accountRecord
	for i := range s.accounts {
		if s.accounts[i].Operator == operator && s.accounts[i].PublicKey == accountPublicKey {
			a := s.accounts[i]
			acc = &a
			break
		}
	}
	s.mu.RUnlock()
	if acc == nil {
		return nil, fmt.Errorf("account not found")
	}
	sigKP, err := nkeys.CreateAccount()
	if err != nil {
		return nil, fmt.Errorf("create signing keypair: %w", err)
	}
	sigSeed, err := sigKP.Seed()
	if err != nil {
		return nil, fmt.Errorf("export signing seed: %w", err)
	}
	if err := s.store.SaveAccountSigningKey(operator, acc.Name, accountPublicKey, string(sigSeed)); err != nil {
		return nil, fmt.Errorf("persist signing key: %w", err)
	}
	// Re-push account JWT so NATS knows the new signing key.
	if err := s.pushAccountToNATS(*acc); err != nil {
		// Best-effort: signing key is stored, so future re-pushes can pick it up.
		log.Printf("ensureAccountSigningKey: push account JWT for %s/%s: %v", operator, accountPublicKey, err)
	}
	return &store.AccountSigningKey{
		Operator:         operator,
		Account:          acc.Name,
		AccountPublicKey: accountPublicKey,
		Seed:             string(sigSeed),
	}, nil
}

func (s *Server) systemAccountPublicKey() string {
	seed := strings.TrimSpace(s.cfg.NATSSysNKey)
	if seed == "" {
		return ""
	}
	kp, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return ""
	}
	pub, err := kp.PublicKey()
	if err != nil {
		return ""
	}
	return pub
}

func buildUserCreds(userName, userPublicKey, userSeed, accountSigningSeed, accountPublicKey string) (string, error) {
	accountKP, err := nkeys.FromSeed([]byte(accountSigningSeed))
	if err != nil {
		return "", fmt.Errorf("invalid account signing seed: %w", err)
	}
	sigPub, err := accountKP.PublicKey()
	if err != nil {
		return "", fmt.Errorf("derive signing public key: %w", err)
	}
	claims := natsjwt.NewUserClaims(userPublicKey)
	claims.Name = userName
	claims.IssuedAt = time.Now().Unix()
	if sigPub != accountPublicKey {
		claims.IssuerAccount = accountPublicKey
	}
	userJWT, err := claims.Encode(accountKP)
	if err != nil {
		return "", fmt.Errorf("encode user jwt: %w", err)
	}
	return fmt.Sprintf(`-----BEGIN NATS USER JWT-----
%s
------END NATS USER JWT------

************************* IMPORTANT *************************
NKEY Seed printed below can be used to sign and prove identity.
NKEYs are sensitive and should be treated as secrets.

-----BEGIN USER NKEY SEED-----
%s
------END USER NKEY SEED------

*************************************************************
`, userJWT, userSeed), nil
}

// LoadFromNATS queries the NATS resolver to populate in-memory operators and accounts.
func (s *Server) LoadFromNATS() {
	if s.natsConn == nil || !s.natsConn.IsConnected() {
		log.Println("LoadFromNATS: skipped (no NATS connection)")
		return
	}
	msg, err := s.natsConn.Request("$SYS.REQ.CLAIMS.LIST", nil, 3*time.Second)
	if err != nil {
		log.Printf("LoadFromNATS: failed to list accounts from resolver: %v", err)
		return
	}
	var listResp struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(msg.Data, &listResp); err != nil {
		log.Printf("LoadFromNATS: failed to parse account list: %v", err)
		return
	}
	keys := listResp.Data

	operatorName := "default"
	if pingMsg, err := s.natsConn.Request("$SYS.REQ.SERVER.PING", nil, 2*time.Second); err == nil {
		var serverInfo struct {
			Server struct {
				Operator string `json:"operator"`
			} `json:"server"`
		}
		if json.Unmarshal(pingMsg.Data, &serverInfo) == nil && serverInfo.Server.Operator != "" {
			operatorName = serverInfo.Server.Operator
		}
	}

	var loadedAccounts []accountRecord
	systemAccount := s.systemAccountPublicKey()
	for _, pubKey := range keys {
		lookupMsg, err := s.natsConn.Request(
			"$SYS.REQ.ACCOUNT."+pubKey+".CLAIMS.LOOKUP",
			nil,
			2*time.Second,
		)
		if err != nil {
			log.Printf("LoadFromNATS: failed to lookup account %s: %v", pubKey, err)
			continue
		}
		acClaims, err := natsjwt.DecodeAccountClaims(string(lookupMsg.Data))
		if err != nil {
			log.Printf("LoadFromNATS: failed to decode account JWT for %s: %v", pubKey, err)
			continue
		}
		jsEnabled := acClaims.Account.Limits.IsJSEnabled()
		loadedAccounts = append(loadedAccounts, accountRecord{
			Name:           acClaims.Name,
			Operator:       operatorName,
			PublishAllow:   append([]string{}, acClaims.Account.DefaultPermissions.Pub.Allow...),
			SubscribeAllow: append([]string{}, acClaims.Account.DefaultPermissions.Sub.Allow...),
			PublicKey:      acClaims.Subject,
			IsSystem:       strings.EqualFold(acClaims.Subject, systemAccount),
			JSEnabled:      jsEnabled,
		})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if operatorName != "" {
		found := false
		for _, op := range s.operators {
			if op.Name == operatorName {
				found = true
				break
			}
		}
		if !found {
			s.operators = append(s.operators, operatorRecord{Name: operatorName})
		}
	}

	existing := map[string]int{}
	for i, acc := range s.accounts {
		existing[acc.Operator+"/"+acc.Name] = i
	}
	for _, acc := range loadedAccounts {
		k := acc.Operator + "/" + acc.Name
		if idx, found := existing[k]; found {
			// update mutable fields from JWT
			s.accounts[idx].PublishAllow = acc.PublishAllow
			s.accounts[idx].SubscribeAllow = acc.SubscribeAllow
			s.accounts[idx].JSEnabled = acc.JSEnabled
		} else {
			s.accounts = append(s.accounts, acc)
		}
	}
	log.Printf("LoadFromNATS: loaded %d accounts for operator %q", len(loadedAccounts), operatorName)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJetStreamError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, nats.ErrStreamNotFound) {
		writeError(w, http.StatusNotFound, "stream not found")
		return
	}

	var apiErr *nats.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode {
		case 10059:
			writeError(w, http.StatusNotFound, "stream not found")
			return
		case 10014:
			writeError(w, http.StatusNotFound, "consumer not found")
			return
		}
	}

	if strings.TrimSpace(fallback) == "" {
		fallback = "jetstream request failed"
	}
	writeError(w, http.StatusBadRequest, fallback)
}
