package accounts

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"

	"github.com/iterating-io/nats-console/api/internal/store"
)

type Handler struct {
	repo    *Repository
	service *Service
	store   *store.Store
}

func NewHandler(repo *Repository, service *Service, st *store.Store) *Handler {
	return &Handler{repo: repo, service: service, store: st}
}

func (h *Handler) ListOperators(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"operators": h.repo.ListOperators()})
}

func (h *Handler) ListAccounts(w http.ResponseWriter, _ *http.Request) {
	h.refreshNATSCapabilitiesIfStale(2 * time.Second)
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts":     h.repo.ListAccounts(),
		"capabilities": h.repo.Capabilities(),
	})
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
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
	if !h.repo.OperatorExists(req.Operator) {
		writeError(w, http.StatusBadRequest, "operator not found")
		return
	}
	if h.repo.AccountNameExists(req.Operator, req.Name) {
		writeError(w, http.StatusConflict, "account already exists in this operator")
		return
	}
	accountKP, err := nkeys.CreateAccount()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create account key")
		return
	}
	pubKey, err := accountKP.PublicKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create account key")
		return
	}
	seed, err := accountKP.Seed()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create account key")
		return
	}
	if err := h.store.SaveAccountSigningKey(req.Operator, req.Name, pubKey, string(seed)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist account signing key")
		return
	}
	record := Record{
		Name:           req.Name,
		Operator:       req.Operator,
		PublishAllow:   UniqueTrimmedSubjects(req.PublishAllow),
		SubscribeAllow: UniqueTrimmedSubjects(req.SubscribeAllow),
		PublicKey:      pubKey,
	}
	h.repo.AddAccount(record)
	if err := h.ensureStreamReaderUser(record); err != nil {
		h.repo.RemoveAccount(record.Operator, record.PublicKey)
		if cleanupErr := h.store.DeleteAccountData(req.Operator, pubKey); cleanupErr != nil {
			log.Printf("createAccount: cleanup data for %s/%s: %v", req.Operator, req.Name, cleanupErr)
		}
		log.Printf("createAccount: provision stream reader for %s/%s: %v", req.Operator, req.Name, err)
		writeError(w, http.StatusInternalServerError, "failed to provision stream reader user")
		return
	}
	if err := h.service.PushAccountToNATS(record); err != nil {
		h.repo.RemoveAccount(record.Operator, record.PublicKey)
		if cleanupErr := h.store.DeleteAccountData(req.Operator, pubKey); cleanupErr != nil {
			log.Printf("createAccount: cleanup data for %s/%s: %v", req.Operator, req.Name, cleanupErr)
		}
		log.Printf("createAccount: push JWT for %s/%s: %v", req.Operator, req.Name, err)
		writeError(w, http.StatusBadGateway, "failed to update nats resolver")
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (h *Handler) GetAccountJWT(w http.ResponseWriter, r *http.Request) {
	operator := r.PathValue("operator")
	accountPublicKey := r.PathValue("accountPublicKey")
	if operator == "" || accountPublicKey == "" {
		writeError(w, http.StatusBadRequest, "operator and accountPublicKey are required")
		return
	}
	rawJWT, err := h.service.LookupAccountJWT(accountPublicKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "account JWT not found")
		return
	}
	claims, err := natsjwt.DecodeAccountClaims(rawJWT)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode account JWT")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jwt": rawJWT, "payload": claims})
}

func (h *Handler) ToggleAccountJetStream(w http.ResponseWriter, r *http.Request) {
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
	if err := h.service.ToggleAccountJetStream(accountPublicKey, req.Enabled); err != nil {
		writeError(w, http.StatusNotFound, "account JWT not found")
		return
	}
	h.repo.UpdateJetStream(operator, accountPublicKey, req.Enabled)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled, "message": "JetStream updated for account"})
}

func (h *Handler) GrantJetStreamSource(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	sourceAccount := strings.TrimSpace(r.PathValue("accountPublicKey"))
	var req struct {
		TargetAccountPublicKey string `json:"targetAccountPublicKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetAccount := strings.TrimSpace(req.TargetAccountPublicKey)
	if operator == "" || sourceAccount == "" || targetAccount == "" {
		writeError(w, http.StatusBadRequest, "source and target accounts are required")
		return
	}
	if _, ok := h.repo.FindByPublicKey(operator, sourceAccount); !ok {
		writeError(w, http.StatusNotFound, "source account not found")
		return
	}
	if err := h.service.GrantJetStreamSource(sourceAccount, targetAccount); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "JetStream source access granted"})
}

func (h *Handler) ToggleJetStreamSource(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.ToggleJetStreamSource(accountPublicKey, req.Enabled); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.repo.UpdateSourceEnabled(operator, accountPublicKey, req.Enabled)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled})
}

func (h *Handler) ListJetStreamSourceAccounts(w http.ResponseWriter, r *http.Request) {
	targetAccount := strings.TrimSpace(r.URL.Query().Get("accountPublicKey"))
	if targetAccount == "" {
		writeError(w, http.StatusBadRequest, "accountPublicKey query parameter is required")
		return
	}
	available := []Record{}
	for _, candidate := range h.repo.ListAccounts() {
		if !candidate.JSEnabled || !candidate.SourceEnabled {
			continue
		}
		if candidate.PublicKey == targetAccount {
			available = append(available, candidate)
			continue
		}
		claims, err := h.service.LookupAccountClaims(candidate.PublicKey)
		if err != nil {
			continue
		}
		for _, granted := range SourceExportTargets(claims) {
			if granted == targetAccount {
				available = append(available, candidate)
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": available})
}

func (h *Handler) ListJetStreamSourceImports(w http.ResponseWriter, r *http.Request) {
	targetAccount := strings.TrimSpace(r.PathValue("accountPublicKey"))
	claims, err := h.service.LookupAccountClaims(targetAccount)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	sources := []Record{}
	for _, sourceKey := range SourceImportAccounts(claims) {
		if source, ok := h.repo.FindAnyByPublicKey(sourceKey); ok {
			sources = append(sources, source)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	operator := strings.TrimSpace(r.PathValue("operator"))
	accountPublicKey := strings.TrimSpace(r.PathValue("accountPublicKey"))
	if operator == "" || accountPublicKey == "" {
		writeError(w, http.StatusBadRequest, "operator and accountPublicKey are required")
		return
	}
	if strings.EqualFold(accountPublicKey, h.service.SystemAccountPublicKey()) {
		writeError(w, http.StatusForbidden, "system account cannot be deleted")
		return
	}
	h.refreshNATSCapabilitiesIfStale(2 * time.Second)
	if !h.repo.Capabilities().AccountDelete {
		writeError(w, http.StatusPreconditionFailed, "account delete is disabled in NATS resolver configuration")
		return
	}
	if _, ok := h.repo.FindByPublicKey(operator, accountPublicKey); !ok {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err := h.service.DeleteAccountInNATS(accountPublicKey); err != nil {
		log.Printf("deleteAccount: failed for %s/%s: %v", operator, accountPublicKey, err)
		writeError(w, http.StatusBadGateway, "failed to delete account in nats")
		return
	}
	h.repo.RemoveAccount(operator, accountPublicKey)
	if err := h.store.DeleteAccountData(operator, accountPublicKey); err != nil {
		log.Printf("deleteAccount: local cleanup failed for %s/%s: %v", operator, accountPublicKey, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddPublishAllow(w http.ResponseWriter, r *http.Request) {
	h.updateAllow(w, r, h.repo.AddPublishAllow)
}

func (h *Handler) RemovePublishAllow(w http.ResponseWriter, r *http.Request) {
	h.removeAllow(w, r, h.repo.RemovePublishAllow)
}

func (h *Handler) AddSubscribeAllow(w http.ResponseWriter, r *http.Request) {
	h.updateAllow(w, r, h.repo.AddSubscribeAllow)
}

func (h *Handler) RemoveSubscribeAllow(w http.ResponseWriter, r *http.Request) {
	h.removeAllow(w, r, h.repo.RemoveSubscribeAllow)
}
