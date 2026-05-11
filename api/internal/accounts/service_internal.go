package accounts

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

func (s *Service) natsConn() NATSClient {
	if s.natsRef == nil {
		return nil
	}
	return s.natsRef()
}

func (s *Service) detectAccountDeleteSupport() (bool, error) {
	if s.cfg.OperatorNKey == "" {
		return false, fmt.Errorf("OPERATOR_NKEY not configured")
	}
	nc := s.natsConn()
	if nc == nil || !nc.IsConnected() {
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
	msg, err := nc.Request("$SYS.REQ.CLAIMS.DELETE", []byte(j), 5*time.Second)
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
