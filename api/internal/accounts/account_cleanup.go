package accounts

import (
	"context"
	"fmt"
	"strings"
	"time"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// CleanupAccountResources removes every JetStream resource and managed
// cross-account source relationship involving account before its claim is removed.
func (s *Service) CleanupAccountResources(account Record) error {
	for _, target := range s.repo.ListAccounts() {
		if target.PublicKey == account.PublicKey || !target.JSEnabled {
			continue
		}
		if err := s.removeAccountFromStreamSources(target, account.PublicKey); err != nil {
			return fmt.Errorf("remove source references from %s: %w", target.Name, err)
		}
	}
	if err := s.removeAccountSourceClaims(account.PublicKey); err != nil {
		return fmt.Errorf("remove source access: %w", err)
	}
	if account.JSEnabled {
		if err := s.deleteAllAccountStreams(account); err != nil {
			return fmt.Errorf("delete streams from %s: %w", account.Name, err)
		}
	}
	return nil
}

func (s *Service) removeAccountFromStreamSources(target Record, sourceAccount string) error {
	nc, err := s.connectAsAccount(target, "nats-console-account-delete-source-cleanup")
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for name := range js.StreamNames(nats.Context(ctx)) {
		info, err := js.StreamInfo(name, nats.Context(ctx))
		if err != nil {
			return fmt.Errorf("read stream %s: %w", name, err)
		}
		cfg, changed := removeExternalAccountSources(info.Config, sourceAccount)
		if !changed {
			continue
		}
		if _, err := js.UpdateStream(&cfg, nats.Context(ctx)); err != nil {
			return fmt.Errorf("update stream %s: %w", name, err)
		}
	}
	return ctx.Err()
}

func removeExternalAccountSources(cfg nats.StreamConfig, accountPublicKey string) (nats.StreamConfig, bool) {
	apiPrefix := sourceAPIPrefix(accountPublicKey)
	filtered := make([]*nats.StreamSource, 0, len(cfg.Sources))
	changed := false
	for _, source := range cfg.Sources {
		if source != nil && source.External != nil && strings.TrimSpace(source.External.APIPrefix) == apiPrefix {
			changed = true
			continue
		}
		filtered = append(filtered, source)
	}
	if changed {
		cfg.Sources = filtered
	}
	if cfg.Mirror != nil && cfg.Mirror.External != nil && strings.TrimSpace(cfg.Mirror.External.APIPrefix) == apiPrefix {
		cfg.Mirror = nil
		changed = true
	}
	return cfg, changed
}

func (s *Service) removeAccountSourceClaims(accountPublicKey string) error {
	op, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return fmt.Errorf("invalid operator nkey: %w", err)
	}
	for _, account := range s.repo.ListAccounts() {
		if account.PublicKey == accountPublicKey {
			continue
		}
		claims, err := s.LookupAccountClaims(account.PublicKey)
		if err != nil {
			return fmt.Errorf("lookup %s claims: %w", account.Name, err)
		}
		importsBefore, exportsBefore := len(claims.Imports), len(claims.Exports)
		claims.Imports = removeImports(claims.Imports,
			sourceAPIImportName(accountPublicKey),
			sourceDeliveryImportName(accountPublicKey),
			sourceFlowControlImportName(accountPublicKey),
		)
		claims.Exports = removeExports(claims.Exports, sourceDeliveryExportName(accountPublicKey))
		if len(claims.Imports) == importsBefore && len(claims.Exports) == exportsBefore {
			continue
		}
		claims.IssuedAt = time.Now().Unix()
		if _, err := s.PushAccountClaimsToNATS(claims, op); err != nil {
			return fmt.Errorf("update %s claims: %w", account.Name, err)
		}
	}
	return nil
}

func (s *Service) deleteAllAccountStreams(account Record) error {
	nc, err := s.connectAsAccount(account, "nats-console-account-delete-stream-cleanup")
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	names := []string{}
	for name := range js.StreamNames(nats.Context(ctx)) {
		names = append(names, name)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, name := range names {
		if err := js.DeleteStream(name, nats.Context(ctx)); err != nil {
			return fmt.Errorf("delete stream %s: %w", name, err)
		}
	}
	return nil
}

func (s *Service) connectAsAccount(account Record, connectionName string) (*nats.Conn, error) {
	signingKey, err := s.EnsureAccountSigningKey(account.Operator, account.PublicKey)
	if err != nil {
		return nil, err
	}
	accountKP, err := nkeys.FromSeed([]byte(signingKey.Seed))
	if err != nil {
		return nil, err
	}
	userKP, err := nkeys.CreateUser()
	if err != nil {
		return nil, err
	}
	userPublicKey, err := userKP.PublicKey()
	if err != nil {
		return nil, err
	}
	claims := natsjwt.NewUserClaims(userPublicKey)
	claims.Name = connectionName
	claims.Expires = time.Now().Add(2 * time.Minute).Unix()
	signerPublicKey, err := accountKP.PublicKey()
	if err != nil {
		return nil, err
	}
	if signerPublicKey != account.PublicKey {
		claims.IssuerAccount = account.PublicKey
	}
	userJWT, err := claims.Encode(accountKP)
	if err != nil {
		return nil, err
	}
	return nats.Connect(s.cfg.NATSURL, nats.Name(connectionName), nats.Timeout(10*time.Second), nats.UserJWT(
		func() (string, error) { return userJWT, nil },
		func(nonce []byte) ([]byte, error) { return userKP.Sign(nonce) },
	))
}
