package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"

	"github.com/iterating-io/nats-console/api/internal/accounts"
	"github.com/iterating-io/nats-console/api/internal/auth"
	"github.com/iterating-io/nats-console/api/internal/config"
	"github.com/iterating-io/nats-console/api/internal/jetstream"
	"github.com/iterating-io/nats-console/api/internal/store"
	"github.com/iterating-io/nats-console/api/internal/users"
)

type natsClient interface {
	IsConnected() bool
	Request(subj string, data []byte, timeout time.Duration) (*nats.Msg, error)
	Publish(subj string, data []byte) error
}

type Server struct {
	cfg              config.Config
	jwtSvc           auth.JWTService
	natsConn         natsClient
	store            *store.Store
	authHandler      *auth.Handler
	accountsRepo     *accounts.Repository
	accountsService  *accounts.Service
	accountsHandler  *accounts.Handler
	usersHandler     *users.Handler
	jetstreamHandler *jetstream.Handler
}

func New(cfg config.Config, jwtSvc auth.JWTService, natsConn natsClient, st *store.Store) *Server {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			allowed := strings.TrimSpace(cfg.AllowedOrigins)
			if allowed == "*" || allowed == "" {
				return true
			}
			return strings.EqualFold(r.Header.Get("Origin"), allowed)
		},
	}
	server := &Server{cfg: cfg, jwtSvc: jwtSvc, natsConn: natsConn, store: st}
	server.authHandler = auth.NewHandler(cfg, jwtSvc, func() auth.NATSClient { return server.natsConn }, upgrader)
	server.accountsRepo = accounts.NewRepository()
	server.accountsService = accounts.NewService(cfg, st, server.accountsRepo, func() accounts.NATSClient { return server.natsConn })
	server.accountsHandler = accounts.NewHandler(server.accountsRepo, server.accountsService, st)
	server.usersHandler = users.NewHandler(st, server.accountsRepo, server.accountsService, func() users.NATSClient { return server.natsConn })
	server.jetstreamHandler = jetstream.NewHandler(cfg, server.accountsRepo, server.accountsService)
	return server
}

func (s *Server) SetNATSConn(nc natsClient) {
	s.natsConn = nc
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

func (s *Server) LoadFromNATS() {
	s.accountsService.LoadFromNATS()
}

func (s *Server) RefreshNATSCapabilities() {
	s.accountsService.RefreshNATSCapabilities()
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
