package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nats-io/nkeys"

	"github.com/iterating-io/nats-console/api/internal/store"
)

func (h *Handler) GetAsyncAPI(w http.ResponseWriter, _ *http.Request) {
	operator := singleOperator(h.repo)
	checker, exists := h.repo.FindByName(operator, asyncAPICheckerAccountName)
	imports, err := h.service.AsyncAPIImports()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checker": checker, "checkerExists": exists, "imports": imports})
}

func (h *Handler) EnsureAsyncAPI(w http.ResponseWriter, _ *http.Request) {
	operator := singleOperator(h.repo)
	if checker, ok := h.repo.FindByName(operator, asyncAPICheckerAccountName); ok {
		if err := h.ensureAsyncAPICheckerUser(checker); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"checker": checker, "created": false})
		return
	}
	if !h.repo.OperatorExists(operator) {
		writeError(w, http.StatusBadRequest, "operator not found")
		return
	}
	accountKP, err := nkeys.CreateAccount()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create checker account")
		return
	}
	pub, err := accountKP.PublicKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create checker account")
		return
	}
	seed, err := accountKP.Seed()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create checker account")
		return
	}
	if err := h.store.SaveAccountSigningKey(operator, asyncAPICheckerAccountName, pub, string(seed)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store checker account")
		return
	}
	checker := Record{Name: asyncAPICheckerAccountName, Operator: operator, PublicKey: pub}
	h.repo.AddAccount(checker)
	if err := h.ensureAsyncAPICheckerUser(checker); err != nil {
		h.repo.RemoveAccount(operator, pub)
		_ = h.store.DeleteAccountData(operator, pub)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.service.PushAccountToNATS(checker); err != nil {
		h.repo.RemoveAccount(operator, pub)
		_ = h.store.DeleteAccountData(operator, pub)
		writeError(w, http.StatusBadGateway, "failed to update nats resolver")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"checker": checker, "created": true})
}

func (h *Handler) ToggleAsyncAPIImport(w http.ResponseWriter, r *http.Request) {
	accountKey := r.PathValue("accountPublicKey")
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.SetAsyncAPIStreamInfoImport(accountKey, req.Enabled); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled})
}

func (h *Handler) ensureAsyncAPICheckerUser(acc Record) error {
	if _, err := h.store.GetUser(acc.Operator, acc.PublicKey, asyncAPICheckerUserName); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	kp, err := nkeys.CreateUser()
	if err != nil {
		return fmt.Errorf("create checker user: %w", err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		return fmt.Errorf("checker user public key: %w", err)
	}
	seed, err := kp.Seed()
	if err != nil {
		return fmt.Errorf("checker user seed: %w", err)
	}
	if _, err := h.store.CreateUser(acc.Operator, acc.Name, acc.PublicKey, asyncAPICheckerUserName, pub, string(seed)); err != nil {
		return fmt.Errorf("persist checker user: %w", err)
	}
	if _, err := h.store.AddUserSubscribeAllow(acc.Operator, acc.PublicKey, asyncAPICheckerUserName, "_INBOX.>"); err != nil {
		return fmt.Errorf("persist checker inbox access: %w", err)
	}
	return nil
}
