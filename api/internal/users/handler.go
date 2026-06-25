package users

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	"github.com/iterating-io/nats-console/api/internal/accounts"
	"github.com/iterating-io/nats-console/api/internal/auth"
	"github.com/iterating-io/nats-console/api/internal/store"
)

type NATSClient interface {
	IsConnected() bool
	Request(subj string, data []byte, timeout time.Duration) (*nats.Msg, error)
	Publish(subj string, data []byte) error
}

type Handler struct {
	store        *store.Store
	accountsRepo *accounts.Repository
	accountsSvc  *accounts.Service
	natsRef      func() NATSClient
}

func NewHandler(st *store.Store, accountsRepo *accounts.Repository, accountsSvc *accounts.Service, natsRef func() NATSClient) *Handler {
	return &Handler{store: st, accountsRepo: accountsRepo, accountsSvc: accountsSvc, natsRef: natsRef}
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	users, err := h.store.ListUsers(operator, accountPublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
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
	account, ok := h.accountsRepo.FindByPublicKey(operator, accountPublicKey)
	if !ok {
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
	record, err := h.store.CreateUser(operator, account.Name, account.PublicKey, req.Name, pubKey, string(seed))
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

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	user := strings.TrimSpace(r.PathValue("user"))
	u, err := h.store.GetUser(operator, accountPublicKey, user)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if err := h.accountsSvc.RevokeUserInNATS(accountPublicKey, u.PublicKey); err != nil {
		log.Printf("deleteUser: revoke failed for %s/%s/%s (%s): %v", operator, accountPublicKey, user, u.PublicKey, err)
		writeError(w, http.StatusBadGateway, "failed to revoke user in nats")
		return
	}
	if err := h.store.DeleteUser(operator, accountPublicKey, user); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddUserPublishAllow(w http.ResponseWriter, r *http.Request) {
	h.updateUserPublishAllow(w, r, h.store.AddUserPublishAllow)
}

func (h *Handler) RemoveUserPublishAllow(w http.ResponseWriter, r *http.Request) {
	h.updateUserPublishAllow(w, r, h.store.RemoveUserPublishAllow)
}

func (h *Handler) AddUserPublishDeny(w http.ResponseWriter, r *http.Request) {
	h.updateUserPublishAllow(w, r, h.store.AddUserPublishDeny)
}

func (h *Handler) RemoveUserPublishDeny(w http.ResponseWriter, r *http.Request) {
	h.updateUserPublishAllow(w, r, h.store.RemoveUserPublishDeny)
}

func (h *Handler) ListAllUsers(w http.ResponseWriter, _ *http.Request) {
	list, err := h.store.ListAllUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": list})
}

func (h *Handler) PublishAsUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
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
	nc := h.natsConn()
	if nc == nil || !nc.IsConnected() {
		writeError(w, http.StatusBadGateway, "nats is not connected")
		return
	}
	acc, ok := h.accountsRepo.FindByName(req.Operator, req.Account)
	if !ok {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	usr, err := h.store.GetUser(req.Operator, acc.PublicKey, req.User)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	accountAllowed := len(acc.PublishAllow) == 0 || accounts.SubjectAllowed(req.Subject, acc.PublishAllow)
	userAllowed := len(usr.PublishAllow) == 0 || accounts.SubjectAllowed(req.Subject, usr.PublishAllow)
	if !accountAllowed || !userAllowed {
		writeError(w, http.StatusForbidden, "subject is not allowed for this user")
		return
	}
	if err := nc.Publish(req.Subject, []byte(req.Message)); err != nil {
		writeError(w, http.StatusBadGateway, "failed to publish")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"published": true, "subject": req.Subject, "by": claims.Username, "asUser": req.User, "account": req.Account, "operator": req.Operator})
}

func (h *Handler) GetUserCreds(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	userName := strings.TrimSpace(r.PathValue("user"))
	userSeed, err := h.store.GetUserSeed(operator, accountPublicKey, userName)
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
	user, err := h.store.GetUser(operator, accountPublicKey, userName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	signingKey, err := h.accountsSvc.EnsureAccountSigningKey(operator, accountPublicKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to prepare account signing key: "+err.Error())
		return
	}
	creds, err := buildUserCreds(
		user.Name,
		user.PublicKey,
		userSeed,
		signingKey.Seed,
		accountPublicKey,
		user.PublishAllow,
		user.PublishDeny,
		user.SubscribeAllow,
	)
	if err != nil {
		log.Printf("getUserCreds: %s/%s/%s: %v", operator, accountPublicKey, userName, err)
		writeError(w, http.StatusInternalServerError, "failed to generate creds")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"creds": creds})
}
