package accounts

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"

	"github.com/iterating-io/nats-console/api/internal/store"
)

const (
	asyncAPICheckerAccountName = "asyncapi-checker"
	asyncAPICheckerUserName    = "asyncapi-checker"
	asyncAPISharePrefix        = "nats-console-asyncapi-stream-info-"
)

func asyncAPIExportName() string              { return asyncAPISharePrefix + "export" }
func asyncAPIImportName(source string) string { return asyncAPISharePrefix + "import-" + source }

func asyncAPIAlias(accountName string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(accountName) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		name = "account"
	}
	return "checker." + name + ".$JS.API.STREAM.INFO.>"
}

func (s *Service) SetAsyncAPIStreamInfoImport(sourceAccount string, enabled bool) error {
	checker, ok := s.repo.FindByName(singleOperator(s.repo), asyncAPICheckerAccountName)
	if !ok {
		return fmt.Errorf("asyncapi checker account has not been created")
	}
	source, ok := s.repo.FindAnyByPublicKey(sourceAccount)
	if !ok || source.IsSystem || source.PublicKey == checker.PublicKey {
		return fmt.Errorf("source account not found")
	}
	if enabled && !source.JSEnabled {
		return fmt.Errorf("JetStream is disabled for this account")
	}

	checkerClaims, err := s.LookupAccountClaims(checker.PublicKey)
	if err != nil {
		return err
	}
	sourceClaims, err := s.LookupAccountClaims(source.PublicKey)
	if err != nil {
		return err
	}

	alias := asyncAPIAlias(source.Name)
	checkerClaims.Imports = removeImports(checkerClaims.Imports, asyncAPIImportName(source.PublicKey))
	if enabled {
		for _, existing := range sourceClaims.Exports {
			if existing.Type == natsjwt.Service && existing.Subject == "$JS.API.STREAM.INFO.>" && existing.Name != asyncAPIExportName() {
				return fmt.Errorf("Stream Info API is already exported by %q", existing.Name)
			}
		}
	}
	sourceClaims.Exports = removeExports(sourceClaims.Exports, asyncAPIExportName())

	if enabled {
		key, err := s.store.GetAccountSigningKey(source.Operator, source.PublicKey)
		if err != nil {
			return fmt.Errorf("get source signing key: %w", err)
		}
		signer, err := nkeys.FromSeed([]byte(key.Seed))
		if err != nil {
			return fmt.Errorf("load source signing key: %w", err)
		}
		token, err := sourceActivationToken(checker.PublicKey, source.PublicKey, "$JS.API.STREAM.INFO.>", natsjwt.Service, signer)
		if err != nil {
			return err
		}
		sourceClaims.Exports.Add(&natsjwt.Export{Name: asyncAPIExportName(), Subject: "$JS.API.STREAM.INFO.>", Type: natsjwt.Service, TokenReq: true})
		checkerClaims.Imports.Add(&natsjwt.Import{Name: asyncAPIImportName(source.PublicKey), Account: source.PublicKey, Subject: "$JS.API.STREAM.INFO.>", LocalSubject: natsjwt.RenamingSubject(alias), Type: natsjwt.Service, Token: token})
	}

	sourceClaims.IssuedAt, checkerClaims.IssuedAt = time.Now().Unix(), time.Now().Unix()
	op, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return fmt.Errorf("invalid operator key: %w", err)
	}
	if _, err = s.PushAccountClaimsToNATS(sourceClaims, op); err != nil {
		return err
	}
	if _, err = s.PushAccountClaimsToNATS(checkerClaims, op); err != nil {
		return err
	}
	if enabled {
		if _, err := s.store.AddUserPublishAllow(checker.Operator, checker.PublicKey, asyncAPICheckerUserName, alias); err != nil && !errors.Is(err, store.ErrConflict) {
			return err
		}
	} else {
		if _, err := s.store.RemoveUserPublishAllow(checker.Operator, checker.PublicKey, asyncAPICheckerUserName, alias); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	return nil
}

func (s *Service) AsyncAPIImports() ([]AsyncAPIImport, error) {
	checker, ok := s.repo.FindByName(singleOperator(s.repo), asyncAPICheckerAccountName)
	if !ok {
		return []AsyncAPIImport{}, nil
	}
	claims, err := s.LookupAccountClaims(checker.PublicKey)
	if err != nil {
		return nil, err
	}
	imported := map[string]bool{}
	for _, imp := range claims.Imports {
		if strings.HasPrefix(imp.Name, asyncAPISharePrefix+"import-") {
			imported[strings.TrimPrefix(imp.Name, asyncAPISharePrefix+"import-")] = true
		}
	}
	items := []AsyncAPIImport{}
	for _, account := range s.repo.ListAccounts() {
		if account.PublicKey == checker.PublicKey || account.IsSystem {
			continue
		}
		items = append(items, AsyncAPIImport{Account: account, Imported: imported[account.PublicKey], Alias: asyncAPIAlias(account.Name)})
	}
	return items, nil
}

type AsyncAPIImport struct {
	Account  Record `json:"account"`
	Imported bool   `json:"imported"`
	Alias    string `json:"alias"`
}

func singleOperator(repo *Repository) string {
	ops := repo.ListOperators()
	if len(ops) == 0 {
		return "default"
	}
	return ops[0].Name
}
