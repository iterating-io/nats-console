package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	"github.com/iterating-io/nats-console/api/internal/config"
	"github.com/iterating-io/nats-console/api/internal/store"
)

type NATSClient interface {
	IsConnected() bool
	Request(subj string, data []byte, timeout time.Duration) (*nats.Msg, error)
	Publish(subj string, data []byte) error
}

type Service struct {
	cfg     config.Config
	store   *store.Store
	repo    *Repository
	natsRef func() NATSClient
}

func NewService(cfg config.Config, st *store.Store, repo *Repository, natsRef func() NATSClient) *Service {
	return &Service{cfg: cfg, store: st, repo: repo, natsRef: natsRef}
}

func (s *Service) RefreshNATSCapabilities() {
	accountDelete, err := s.detectAccountDeleteSupport()
	if err != nil {
		log.Printf("RefreshNATSCapabilities: account delete probe failed: %v", err)
	}
	s.repo.SetCapabilities(Capabilities{AccountDelete: accountDelete}, time.Now())
}

func (s *Service) LoadFromNATS() {
	nc := s.natsConn()
	if nc == nil || !nc.IsConnected() {
		log.Println("LoadFromNATS: skipped (no NATS connection)")
		return
	}
	msg, err := nc.Request("$SYS.REQ.CLAIMS.LIST", nil, 3*time.Second)
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

	operatorName := "default"
	if pingMsg, err := nc.Request("$SYS.REQ.SERVER.PING", nil, 2*time.Second); err == nil {
		var serverInfo struct {
			Server struct {
				Operator string `json:"operator"`
			} `json:"server"`
		}
		if json.Unmarshal(pingMsg.Data, &serverInfo) == nil && serverInfo.Server.Operator != "" {
			operatorName = serverInfo.Server.Operator
		}
	}

	loaded := []Record{}
	systemAccount := s.SystemAccountPublicKey()
	for _, pubKey := range listResp.Data {
		lookupMsg, err := nc.Request("$SYS.REQ.ACCOUNT."+pubKey+".CLAIMS.LOOKUP", nil, 2*time.Second)
		if err != nil {
			log.Printf("LoadFromNATS: failed to lookup account %s: %v", pubKey, err)
			continue
		}
		claims, err := natsjwt.DecodeAccountClaims(string(lookupMsg.Data))
		if err != nil {
			log.Printf("LoadFromNATS: failed to decode account JWT for %s: %v", pubKey, err)
			continue
		}
		loaded = append(loaded, Record{
			Name:           claims.Name,
			Operator:       operatorName,
			PublishAllow:   append([]string{}, claims.Account.DefaultPermissions.Pub.Allow...),
			SubscribeAllow: append([]string{}, claims.Account.DefaultPermissions.Sub.Allow...),
			PublicKey:      claims.Subject,
			IsSystem:       strings.EqualFold(claims.Subject, systemAccount),
			JSEnabled:      claims.Account.Limits.IsJSEnabled(),
		})
	}

	if operatorName != "" {
		s.repo.EnsureOperator(operatorName)
	}
	s.repo.UpsertLoaded(loaded)
	log.Printf("LoadFromNATS: loaded %d accounts for operator %q", len(loaded), operatorName)
}

func (s *Service) LookupAccountClaims(accountPublicKey string) (*natsjwt.AccountClaims, error) {
	rawJWT, err := s.LookupAccountJWT(accountPublicKey)
	if err != nil {
		return nil, err
	}
	claims, err := natsjwt.DecodeAccountClaims(rawJWT)
	if err != nil {
		return nil, fmt.Errorf("decode account claims: %w", err)
	}
	return claims, nil
}

func (s *Service) LookupAccountJWT(accountPublicKey string) (string, error) {
	nc := s.natsConn()
	if nc == nil || !nc.IsConnected() {
		return "", fmt.Errorf("NATS not connected")
	}
	lookupMsg, err := nc.Request("$SYS.REQ.ACCOUNT."+accountPublicKey+".CLAIMS.LOOKUP", nil, 2*time.Second)
	if err != nil {
		return "", fmt.Errorf("lookup account claims: %w", err)
	}
	return string(lookupMsg.Data), nil
}

func (s *Service) ToggleAccountJetStream(accountPublicKey string, enabled bool) error {
	if s.cfg.OperatorNKey == "" {
		return fmt.Errorf("OPERATOR_NKEY not configured")
	}
	nc := s.natsConn()
	if nc == nil || !nc.IsConnected() {
		return fmt.Errorf("NATS not connected")
	}
	claims, err := s.LookupAccountClaims(accountPublicKey)
	if err != nil {
		return err
	}
	if enabled {
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
		return fmt.Errorf("invalid OPERATOR_NKEY: %w", err)
	}
	_, err = s.PushAccountClaimsToNATS(claims, opKP)
	return err
}

func (s *Service) PushAccountClaimsToNATS(claims *natsjwt.AccountClaims, opKP nkeys.KeyPair) (*nats.Msg, error) {
	nc := s.natsConn()
	jwt, err := claims.Encode(opKP)
	if err != nil {
		return nil, fmt.Errorf("encode account JWT: %w", err)
	}
	msg, err := nc.Request("$SYS.REQ.CLAIMS.UPDATE", []byte(jwt), 5*time.Second)
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

func (s *Service) PushAccountToNATS(acc Record) error {
	if s.cfg.OperatorNKey == "" {
		return fmt.Errorf("OPERATOR_NKEY not configured")
	}
	nc := s.natsConn()
	if nc == nil || !nc.IsConnected() {
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
	claims, err := s.LookupAccountClaims(pub)
	if err != nil {
		claims = natsjwt.NewAccountClaims(pub)
	}
	claims.Name = acc.Name
	claims.IssuedAt = time.Now().Unix()
	claims.Account.DefaultPermissions.Pub.Allow = append([]string{}, acc.PublishAllow...)
	claims.Account.DefaultPermissions.Sub.Allow = append([]string{}, acc.SubscribeAllow...)
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
	_, err = s.PushAccountClaimsToNATS(claims, opKP)
	return err
}

func (s *Service) RevokeUserInNATS(accountPublicKey, userPublicKey string) error {
	if s.cfg.OperatorNKey == "" {
		return fmt.Errorf("OPERATOR_NKEY not configured")
	}
	nc := s.natsConn()
	if nc == nil || !nc.IsConnected() {
		return fmt.Errorf("NATS not connected")
	}
	opKP, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return fmt.Errorf("invalid operator nkey: %w", err)
	}
	claims, err := s.LookupAccountClaims(accountPublicKey)
	if err != nil {
		return err
	}
	claims.Revoke(strings.TrimSpace(userPublicKey))
	claims.IssuedAt = time.Now().Unix()
	_, err = s.PushAccountClaimsToNATS(claims, opKP)
	return err
}

func (s *Service) DeleteAccountInNATS(accountPublicKey string) error {
	if s.cfg.OperatorNKey == "" {
		return fmt.Errorf("OPERATOR_NKEY not configured")
	}
	nc := s.natsConn()
	if nc == nil || !nc.IsConnected() {
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
	msg, err := nc.Request("$SYS.REQ.CLAIMS.DELETE", []byte(j), 5*time.Second)
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

func (s *Service) EnsureAccountSigningKey(operator, accountPublicKey string) (*store.AccountSigningKey, error) {
	key, err := s.store.GetAccountSigningKey(operator, accountPublicKey)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	acc, ok := s.repo.FindByPublicKey(operator, accountPublicKey)
	if !ok {
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
	if err := s.PushAccountToNATS(acc); err != nil {
		log.Printf("ensureAccountSigningKey: push account JWT for %s/%s: %v", operator, accountPublicKey, err)
	}
	return &store.AccountSigningKey{Operator: operator, Account: acc.Name, AccountPublicKey: accountPublicKey, Seed: string(sigSeed)}, nil
}

func (s *Service) SystemAccountPublicKey() string {
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

func SubjectAllowed(subject string, allowed []string) bool {
	for _, rule := range allowed {
		if MatchSubject(rule, subject) {
			return true
		}
	}
	return false
}

func UniqueTrimmedSubjects(subjects []string) []string {
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

func MatchSubject(rule, subject string) bool {
	if rule == "" || subject == "" {
		return false
	}
	ruleTokens := strings.Split(rule, ".")
	subjectTokens := strings.Split(subject, ".")
	for i, token := range ruleTokens {
		if token == ">" {
			return i == len(ruleTokens)-1
		}
		if i >= len(subjectTokens) {
			return false
		}
		if token != "*" && token != subjectTokens[i] {
			return false
		}
	}
	return len(ruleTokens) == len(subjectTokens)
}
