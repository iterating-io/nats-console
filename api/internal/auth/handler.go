package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"

	"github.com/iterating-io/nats-console/api/internal/config"
)

type NATSClient interface {
	IsConnected() bool
	Request(subj string, data []byte, timeout time.Duration) (*nats.Msg, error)
	Publish(subj string, data []byte) error
}

type JWTService interface {
	Issue(username, role string) (string, error)
	Parse(tokenString string) (*Claims, error)
}

type ClaimsContextKey struct{}

type Handler struct {
	cfg      config.Config
	jwtSvc   JWTService
	natsRef  func() NATSClient
	upgrader websocket.Upgrader
}

func NewHandler(cfg config.Config, jwtSvc JWTService, natsRef func() NATSClient, upgrader websocket.Upgrader) *Handler {
	return &Handler{cfg: cfg, jwtSvc: jwtSvc, natsRef: natsRef, upgrader: upgrader}
}

func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(ClaimsContextKey{}).(*Claims)
	return claims, ok
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username != h.cfg.AdminID || req.Password != h.cfg.AdminPassword {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, err := h.jwtSvc.Issue(req.Username, "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accessToken": token, "role": "admin"})
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
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
	nc := h.natsConn()
	if nc == nil || !nc.IsConnected() {
		writeError(w, http.StatusBadGateway, "nats is not connected")
		return
	}
	if err := nc.Publish(req.Subject, []byte(req.Message)); err != nil {
		writeError(w, http.StatusBadGateway, "failed to publish")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"published": true, "subject": req.Subject, "by": claims.Username})
}

func (h *Handler) WebSocket(w http.ResponseWriter, r *http.Request) {
	tokenString := strings.TrimSpace(r.URL.Query().Get("token"))
	if tokenString == "" {
		writeError(w, http.StatusUnauthorized, "missing token")
		return
	}
	claims, err := h.jwtSvc.Parse(tokenString)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	defer conn.Close()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	if err := conn.WriteJSON(map[string]any{"type": "session", "username": claims.Username, "role": claims.Role, "serverTime": time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return
	}
	for range ticker.C {
		natsState := "disconnected"
		if nc := h.natsConn(); nc != nil && nc.IsConnected() {
			natsState = "connected"
		}
		if err := conn.WriteJSON(map[string]any{"type": "heartbeat", "nats": natsState, "serverTime": time.Now().UTC().Format(time.RFC3339)}); err != nil {
			return
		}
	}
}

func (h *Handler) WithAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
		claims, err := h.jwtSvc.Parse(token)
		if err != nil {
			if errors.Is(err, ErrInvalidToken) {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			writeError(w, http.StatusUnauthorized, "token validation failed")
			return
		}
		ctx := context.WithValue(r.Context(), ClaimsContextKey{}, claims)
		next(w, r.WithContext(ctx))
	}
}
