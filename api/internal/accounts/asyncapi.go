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
	asyncAPICheckerAccountName  = "asyncapi-checker"
	asyncAPICheckerUserName     = "asyncapi-checker"
	asyncAPISharePrefix         = "nats-console-asyncapi-stream-info-"
	asyncAPIConsumerSharePrefix = "nats-console-asyncapi-consumer-info-"
	asyncAPIStatusExportName    = "nats-console-asyncapi-checker-status"
	asyncAPIStatusSubject       = "nats.console.asyncapi.status"
	asyncAPIStatusAlias         = "checker.status.account"
)

func asyncAPIExportName() string              { return asyncAPISharePrefix + "export" }
func asyncAPIImportName(source string) string { return asyncAPISharePrefix + "import-" + source }
func asyncAPIConsumerExportName() string      { return asyncAPIConsumerSharePrefix + "export" }
func asyncAPIConsumerImportName(source string) string {
	return asyncAPIConsumerSharePrefix + "import-" + source
}
func asyncAPIStatusImportName() string { return asyncAPIStatusExportName + "-import" }

// CheckerAccountStatus is the small, checker-visible view of one service
// account. It deliberately contains no account key, JWT, or account listing.
type CheckerAccountStatus struct {
	Exists                    bool                        `json:"exists"`
	JetStreamEnabled          bool                        `json:"jetstreamEnabled"`
	StreamInfoImportEnabled   bool                        `json:"streamInfoImportEnabled"`
	ConsumerInfoImportEnabled bool                        `json:"consumerInfoImportEnabled"`
	PublishAllowed            *bool                       `json:"publishAllowed,omitempty"`
	SourceSharing             *CheckerSourceSharingStatus `json:"sourceSharing,omitempty"`
}

// CheckerSourceSharingStatus is the checker-visible state needed to determine
// whether sourceAccount can be used as a JetStream source by targetAccount.
// It exposes only booleans derived from the two account claims.
type CheckerSourceSharingStatus struct {
	TargetExists                 bool `json:"targetExists"`
	SourceSharingEnabled         bool `json:"sourceSharingEnabled"`
	SourceExportsEnabled         bool `json:"sourceExportsEnabled"`
	ConsumerAPIImportEnabled     bool `json:"consumerAPIImportEnabled"`
	DeliverySubjectImportEnabled bool `json:"deliverySubjectImportEnabled"`
	FlowControlImportEnabled     bool `json:"flowControlImportEnabled"`
}

func (s *Service) CheckerAccountStatus(accountName, sourceTarget, publishSubject string) (CheckerAccountStatus, error) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return CheckerAccountStatus{}, fmt.Errorf("account name is required")
	}
	operator := singleOperator(s.repo)
	source, found := s.repo.FindByName(operator, accountName)
	if !found || source.IsSystem || source.Name == asyncAPICheckerAccountName {
		return CheckerAccountStatus{}, nil
	}
	sourceClaims, err := s.LookupAccountClaims(source.PublicKey)
	if err != nil {
		return CheckerAccountStatus{}, err
	}
	status := CheckerAccountStatus{Exists: true, JetStreamEnabled: sourceClaims.Account.Limits.IsJSEnabled()}
	if subject := strings.TrimSpace(publishSubject); subject != "" {
		allowed := checkerPublishAllowed(sourceClaims, subject)
		status.PublishAllowed = &allowed
	}
	if strings.TrimSpace(sourceTarget) != "" {
		sharing, err := s.checkerSourceSharingStatus(source, sourceClaims, sourceTarget)
		if err != nil {
			return CheckerAccountStatus{}, err
		}
		status.SourceSharing = &sharing
	}
	checker, found := s.repo.FindByName(operator, asyncAPICheckerAccountName)
	if !found {
		return status, nil
	}
	checkerClaims, err := s.LookupAccountClaims(checker.PublicKey)
	if err != nil {
		return CheckerAccountStatus{}, err
	}
	streamImported, consumerImported := false, false
	for _, imp := range checkerClaims.Imports {
		if imp.Name == asyncAPIImportName(source.PublicKey) {
			streamImported = true
		}
		if imp.Name == asyncAPIConsumerImportName(source.PublicKey) {
			consumerImported = true
		}
	}
	status.StreamInfoImportEnabled = streamImported
	status.ConsumerInfoImportEnabled = consumerImported
	return status, nil
}

func checkerConsumerExportPlan(exports natsjwt.Exports) (activationSubject string, addNarrowExport bool) {
	if hasSourceExport(exports, sourceAPIExportName(), "$JS.API.CONSUMER.>", natsjwt.Service) {
		return "$JS.API.CONSUMER.>", false
	}
	return "$JS.API.CONSUMER.INFO.>", true
}

func checkerPublishAllowed(claims *natsjwt.AccountClaims, subject string) bool {
	permissions := claims.Account.DefaultPermissions.Pub
	allowed := len(permissions.Allow) == 0 || SubjectAllowed(subject, permissions.Allow)
	denied := SubjectAllowed(subject, permissions.Deny)
	return allowed && !denied
}

func (s *Service) checkerSourceSharingStatus(source Record, sourceClaims *natsjwt.AccountClaims, targetName string) (CheckerSourceSharingStatus, error) {
	status := CheckerSourceSharingStatus{
		SourceSharingEnabled: sourceClaims.Account.Tags.Contains(sourceEnabledTag),
	}
	target, found := s.repo.FindByName(singleOperator(s.repo), strings.TrimSpace(targetName))
	if !found || target.IsSystem || target.PublicKey == source.PublicKey {
		return status, nil
	}
	status.TargetExists = true
	targetClaims, err := s.LookupAccountClaims(target.PublicKey)
	if err != nil {
		return CheckerSourceSharingStatus{}, err
	}
	deliverySubject := sourceDeliveryPrefix(target.PublicKey) + ".>"
	status.SourceExportsEnabled = hasSourceExport(sourceClaims.Exports, sourceAPIExportName(), "$JS.API.CONSUMER.>", natsjwt.Service) &&
		hasSourceExport(sourceClaims.Exports, sourceDeliveryExportName(target.PublicKey), deliverySubject, natsjwt.Stream) &&
		hasSourceExport(sourceClaims.Exports, sourceFlowControlExportName(), "$JS.FC.>", natsjwt.Service)
	status.ConsumerAPIImportEnabled = hasSourceImport(targetClaims.Imports, sourceAPIImportName(source.PublicKey), source.PublicKey, "$JS.API.CONSUMER.>", natsjwt.Service)
	status.DeliverySubjectImportEnabled = hasSourceImport(targetClaims.Imports, sourceDeliveryImportName(source.PublicKey), source.PublicKey, deliverySubject, natsjwt.Stream)
	status.FlowControlImportEnabled = hasSourceImport(targetClaims.Imports, sourceFlowControlImportName(source.PublicKey), source.PublicKey, "$JS.FC.>", natsjwt.Service)
	return status, nil
}

func hasSourceExport(exports natsjwt.Exports, name, subject string, kind natsjwt.ExportType) bool {
	for _, export := range exports {
		if export.Name == name && export.Subject == natsjwt.Subject(subject) && export.Type == kind && export.TokenReq {
			return true
		}
	}
	return false
}

func hasSourceImport(imports natsjwt.Imports, name, account, subject string, kind natsjwt.ExportType) bool {
	for _, imp := range imports {
		if imp.Name == name && imp.Account == account && imp.Subject == natsjwt.Subject(subject) && imp.Type == kind && imp.Token != "" {
			return true
		}
	}
	return false
}

// EnsureAsyncAPICheckerStatusAccess creates the private system-account service
// import used by the checker status command. It is safe to call repeatedly.
func (s *Service) EnsureAsyncAPICheckerStatusAccess(checker Record) error {
	systemKey := s.SystemAccountPublicKey()
	if systemKey == "" {
		return fmt.Errorf("NATS_SYS_NKEY is required for checker status access")
	}
	checkerClaims, err := s.LookupAccountClaims(checker.PublicKey)
	if err != nil {
		return err
	}
	systemClaims, err := s.LookupAccountClaims(systemKey)
	if err != nil {
		return err
	}
	systemSigner, err := nkeys.FromSeed([]byte(s.cfg.NATSSysNKey))
	if err != nil {
		return fmt.Errorf("invalid NATS_SYS_NKEY: %w", err)
	}
	token, err := sourceActivationToken(checker.PublicKey, systemKey, asyncAPIStatusSubject, natsjwt.Service, systemSigner)
	if err != nil {
		return err
	}
	for _, existing := range systemClaims.Exports {
		if existing.Type == natsjwt.Service && existing.Subject == asyncAPIStatusSubject && existing.Name != asyncAPIStatusExportName {
			return fmt.Errorf("checker status subject is already exported by %q", existing.Name)
		}
	}
	systemClaims.Exports = removeExports(systemClaims.Exports, asyncAPIStatusExportName)
	systemClaims.Exports.Add(&natsjwt.Export{Name: asyncAPIStatusExportName, Subject: asyncAPIStatusSubject, Type: natsjwt.Service, TokenReq: true})
	checkerClaims.Imports = removeImports(checkerClaims.Imports, asyncAPIStatusImportName())
	checkerClaims.Imports.Add(&natsjwt.Import{Name: asyncAPIStatusImportName(), Account: systemKey, Subject: asyncAPIStatusSubject, LocalSubject: asyncAPIStatusAlias, Type: natsjwt.Service, Token: token})
	systemClaims.IssuedAt, checkerClaims.IssuedAt = time.Now().Unix(), time.Now().Unix()
	op, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return fmt.Errorf("invalid operator key: %w", err)
	}
	if _, err = s.PushAccountClaimsToNATS(systemClaims, op); err != nil {
		return err
	}
	if _, err = s.PushAccountClaimsToNATS(checkerClaims, op); err != nil {
		return err
	}
	if _, err = s.store.AddUserPublishAllow(checker.Operator, checker.PublicKey, asyncAPICheckerUserName, asyncAPIStatusAlias); err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	return nil
}

func asyncAPIAlias(accountName string) string {
	return asyncAPISubjectAlias(accountName, "$JS.API.STREAM.INFO.>")
}

func asyncAPIConsumerAlias(accountName string) string {
	return asyncAPISubjectAlias(accountName, "$JS.API.CONSUMER.INFO.>")
}

func asyncAPISubjectAlias(accountName, suffix string) string {
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
	return "checker." + name + "." + suffix
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
	consumerAlias := asyncAPIConsumerAlias(source.Name)
	consumerActivationSubject, addConsumerInfoExport := checkerConsumerExportPlan(sourceClaims.Exports)
	checkerClaims.Imports = removeImports(checkerClaims.Imports, asyncAPIImportName(source.PublicKey))
	checkerClaims.Imports = removeImports(checkerClaims.Imports, asyncAPIConsumerImportName(source.PublicKey))
	if enabled {
		for _, existing := range sourceClaims.Exports {
			if existing.Type == natsjwt.Service && existing.Subject == "$JS.API.STREAM.INFO.>" && existing.Name != asyncAPIExportName() {
				return fmt.Errorf("Stream Info API is already exported by %q", existing.Name)
			}
			if existing.Type == natsjwt.Service && existing.Subject == "$JS.API.CONSUMER.INFO.>" && existing.Name != asyncAPIConsumerExportName() {
				return fmt.Errorf("Consumer Info API is already exported by %q", existing.Name)
			}
		}
	}
	sourceClaims.Exports = removeExports(sourceClaims.Exports, asyncAPIExportName())
	sourceClaims.Exports = removeExports(sourceClaims.Exports, asyncAPIConsumerExportName())

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
		consumerToken, err := sourceActivationToken(checker.PublicKey, source.PublicKey, consumerActivationSubject, natsjwt.Service, signer)
		if err != nil {
			return err
		}
		if addConsumerInfoExport {
			sourceClaims.Exports.Add(&natsjwt.Export{Name: asyncAPIConsumerExportName(), Subject: "$JS.API.CONSUMER.INFO.>", Type: natsjwt.Service, TokenReq: true})
		}
		checkerClaims.Imports.Add(&natsjwt.Import{Name: asyncAPIConsumerImportName(source.PublicKey), Account: source.PublicKey, Subject: "$JS.API.CONSUMER.INFO.>", LocalSubject: natsjwt.RenamingSubject(consumerAlias), Type: natsjwt.Service, Token: consumerToken})
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
		if _, err := s.store.AddUserPublishAllow(checker.Operator, checker.PublicKey, asyncAPICheckerUserName, consumerAlias); err != nil && !errors.Is(err, store.ErrConflict) {
			return err
		}
	} else {
		if _, err := s.store.RemoveUserPublishAllow(checker.Operator, checker.PublicKey, asyncAPICheckerUserName, alias); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if _, err := s.store.RemoveUserPublishAllow(checker.Operator, checker.PublicKey, asyncAPICheckerUserName, consumerAlias); err != nil && !errors.Is(err, store.ErrNotFound) {
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
