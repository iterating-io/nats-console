package accounts

import (
	"fmt"
	"strings"
	"time"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

const sourceSharePrefix = "nats-console-source-"
const sourceEnabledTag = "nats-console-source-enabled"

func sourceAPIExportName() string { return sourceSharePrefix + "api" }
func sourceDeliveryExportName(target string) string {
	return sourceSharePrefix + "delivery-" + target
}
func sourceFlowControlExportName() string      { return sourceSharePrefix + "flow-control" }
func sourceAPIImportName(source string) string { return sourceSharePrefix + "api-" + source }
func sourceDeliveryImportName(source string) string {
	return sourceSharePrefix + "delivery-" + source
}
func sourceFlowControlImportName(source string) string {
	return sourceSharePrefix + "flow-control-" + source
}
func sourceAPIPrefix(source string) string { return "$JS.SOURCE." + source + ".API" }
func sourceDeliveryPrefix(target string) string {
	return "$JS.SOURCE." + target
}

// GrantJetStreamSource configures the private export/import pair required for
// targetAccount to create JetStream sources from sourceAccount.
func (s *Service) GrantJetStreamSource(sourceAccount, targetAccount string) error {
	if sourceAccount == targetAccount {
		return fmt.Errorf("an account does not need a cross-account source grant for itself")
	}
	source, ok := s.repo.FindAnyByPublicKey(sourceAccount)
	if !ok || !source.JSEnabled {
		return fmt.Errorf("source account is not JetStream enabled")
	}
	if !source.SourceEnabled {
		return fmt.Errorf("source account has not enabled source sharing")
	}
	target, ok := s.repo.FindAnyByPublicKey(targetAccount)
	if !ok || !target.JSEnabled {
		return fmt.Errorf("target account is not JetStream enabled")
	}
	sourceClaims, err := s.LookupAccountClaims(sourceAccount)
	if err != nil {
		return err
	}
	targetClaims, err := s.LookupAccountClaims(targetAccount)
	if err != nil {
		return err
	}
	key, err := s.store.GetAccountSigningKey(source.Operator, sourceAccount)
	if err != nil {
		return fmt.Errorf("get source account signing key: %w", err)
	}
	signer, err := nkeys.FromSeed([]byte(key.Seed))
	if err != nil {
		return fmt.Errorf("load source account signing key: %w", err)
	}
	apiToken, err := sourceActivationToken(targetAccount, sourceAccount, "$JS.API.CONSUMER.>", natsjwt.Service, signer)
	if err != nil {
		return err
	}
	deliverySubject := sourceDeliveryPrefix(targetAccount) + ".>"
	deliveryToken, err := sourceActivationToken(targetAccount, sourceAccount, deliverySubject, natsjwt.Stream, signer)
	if err != nil {
		return err
	}
	flowControlToken, err := sourceActivationToken(targetAccount, sourceAccount, "$JS.FC.>", natsjwt.Service, signer)
	if err != nil {
		return err
	}

	removeManagedSourceShare(sourceClaims, targetClaims, sourceAccount, targetAccount)
	normalizeSourceServiceExports(sourceClaims)
	sourceClaims.Exports = removeOrphanedSourceDeliveryExports(sourceClaims.Exports, func(accountPublicKey string) bool {
		_, found := s.repo.FindAnyByPublicKey(accountPublicKey)
		return found
	})
	sourceClaims.Exports.Add(
		&natsjwt.Export{Name: sourceDeliveryExportName(targetAccount), Subject: natsjwt.Subject(deliverySubject), Type: natsjwt.Stream, TokenReq: true},
	)
	ensureSourceServiceExport(sourceClaims, &natsjwt.Export{Name: sourceAPIExportName(), Subject: "$JS.API.CONSUMER.>", Type: natsjwt.Service, ResponseType: natsjwt.ResponseTypeStream, TokenReq: true})
	ensureSourceServiceExport(sourceClaims, &natsjwt.Export{Name: sourceFlowControlExportName(), Subject: "$JS.FC.>", Type: natsjwt.Service, TokenReq: true})
	targetClaims.Imports.Add(
		&natsjwt.Import{Name: sourceAPIImportName(sourceAccount), Account: sourceAccount, Subject: "$JS.API.CONSUMER.>", LocalSubject: natsjwt.RenamingSubject(sourceAPIPrefix(sourceAccount) + ".CONSUMER.>"), Type: natsjwt.Service, Token: apiToken},
		&natsjwt.Import{Name: sourceDeliveryImportName(sourceAccount), Account: sourceAccount, Subject: natsjwt.Subject(deliverySubject), Type: natsjwt.Stream, Token: deliveryToken},
		&natsjwt.Import{Name: sourceFlowControlImportName(sourceAccount), Account: sourceAccount, Subject: "$JS.FC.>", LocalSubject: "$JS.FC.>", Type: natsjwt.Service, Token: flowControlToken},
	)
	sourceClaims.IssuedAt, targetClaims.IssuedAt = time.Now().Unix(), time.Now().Unix()
	op, err := nkeys.FromSeed([]byte(s.cfg.OperatorNKey))
	if err != nil {
		return err
	}
	if _, err = s.PushAccountClaimsToNATS(sourceClaims, op); err != nil {
		return err
	}
	if _, err = s.PushAccountClaimsToNATS(targetClaims, op); err != nil {
		return err
	}
	return nil
}

func sourceActivationToken(target, source, subject string, kind natsjwt.ExportType, signer nkeys.KeyPair) (string, error) {
	activation := natsjwt.NewActivationClaims(target)
	activation.ImportSubject = natsjwt.Subject(subject)
	activation.ImportType = kind
	activation.IssuerAccount = source
	token, err := activation.Encode(signer)
	if err != nil {
		return "", fmt.Errorf("encode source activation: %w", err)
	}
	return token, nil
}

func removeManagedSourceShare(source, target *natsjwt.AccountClaims, sourceKey, targetKey string) {
	source.Exports = removeExports(source.Exports, sourceDeliveryExportName(targetKey))
	target.Imports = removeImports(target.Imports, sourceAPIImportName(sourceKey), sourceDeliveryImportName(sourceKey), sourceFlowControlImportName(sourceKey))
}

// normalizeSourceServiceExports removes legacy target-specific service exports.
// NATS permits one service export per subject, so all targets must share these
// two exports and use their own activation tokens instead.
func normalizeSourceServiceExports(claims *natsjwt.AccountClaims) {
	claims.Exports = filterExports(claims.Exports, func(v *natsjwt.Export) bool {
		return !(strings.HasPrefix(v.Name, sourceSharePrefix+"api-") || strings.HasPrefix(v.Name, sourceSharePrefix+"flow-control-"))
	})
}

func ensureSourceServiceExport(claims *natsjwt.AccountClaims, export *natsjwt.Export) {
	for _, existing := range claims.Exports {
		if existing.Type == export.Type && existing.Subject == export.Subject {
			return
		}
	}
	claims.Exports.Add(export)
}

func removeOrphanedSourceDeliveryExports(exports natsjwt.Exports, accountExists func(string) bool) natsjwt.Exports {
	return filterExports(exports, func(v *natsjwt.Export) bool {
		if !strings.HasPrefix(v.Name, sourceSharePrefix+"delivery-") {
			return true
		}
		target := strings.TrimPrefix(v.Name, sourceSharePrefix+"delivery-")
		return target == "" || accountExists(target)
	})
}
func removeExports(exports natsjwt.Exports, names ...string) natsjwt.Exports {
	return filterExports(exports, func(v *natsjwt.Export) bool {
		for _, name := range names {
			if v.Name == name {
				return false
			}
		}
		return true
	})
}
func filterExports(exports natsjwt.Exports, keep func(*natsjwt.Export) bool) natsjwt.Exports {
	out := natsjwt.Exports{}
	for _, v := range exports {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}
func removeImports(imports natsjwt.Imports, names ...string) natsjwt.Imports {
	out := natsjwt.Imports{}
	for _, v := range imports {
		keep := true
		for _, name := range names {
			if v.Name == name {
				keep = false
			}
		}
		if keep {
			out = append(out, v)
		}
	}
	return out
}

func SourceExportTargets(claims *natsjwt.AccountClaims) []string {
	seen := map[string]bool{}
	targets := []string{}
	for _, export := range claims.Exports {
		if strings.HasPrefix(export.Name, sourceSharePrefix+"api-") {
			target := strings.TrimPrefix(export.Name, sourceSharePrefix+"api-")
			if target != "" && !seen[target] {
				seen[target] = true
				targets = append(targets, target)
			}
		}
	}
	return targets
}

func SourceImportAccounts(claims *natsjwt.AccountClaims) []string {
	seen := map[string]bool{}
	sources := []string{}
	for _, imp := range claims.Imports {
		if strings.HasPrefix(imp.Name, sourceSharePrefix+"api-") {
			source := strings.TrimPrefix(imp.Name, sourceSharePrefix+"api-")
			if source != "" && !seen[source] {
				seen[source] = true
				sources = append(sources, source)
			}
		}
	}
	return sources
}
