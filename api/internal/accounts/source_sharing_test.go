package accounts

import (
	"strings"
	"testing"

	natsjwt "github.com/nats-io/jwt/v2"
)

func TestRemoveManagedSourceShareRemovesFlowControlRoute(t *testing.T) {
	sourceKey, targetKey := "ASOURCE", "BTARGET"
	source := natsjwt.NewAccountClaims(sourceKey)
	target := natsjwt.NewAccountClaims(targetKey)
	source.Exports.Add(
		&natsjwt.Export{Name: sourceAPIExportName()},
		&natsjwt.Export{Name: sourceDeliveryExportName(targetKey)},
		&natsjwt.Export{Name: sourceFlowControlExportName()},
		&natsjwt.Export{Name: "unmanaged-export"},
	)
	target.Imports.Add(
		&natsjwt.Import{Name: sourceAPIImportName(sourceKey)},
		&natsjwt.Import{Name: sourceDeliveryImportName(sourceKey)},
		&natsjwt.Import{Name: sourceFlowControlImportName(sourceKey)},
		&natsjwt.Import{Name: "unmanaged-import"},
	)

	removeManagedSourceShare(source, target, sourceKey, targetKey)

	if len(source.Exports) != 3 || source.Exports[0].Name != sourceAPIExportName() || source.Exports[1].Name != sourceFlowControlExportName() || source.Exports[2].Name != "unmanaged-export" {
		t.Fatalf("source exports = %#v, want shared service exports and unmanaged export", source.Exports)
	}
	if len(target.Imports) != 1 || target.Imports[0].Name != "unmanaged-import" {
		t.Fatalf("target imports = %#v, want only unmanaged import", target.Imports)
	}
}

func TestNormalizeSourceServiceExportsReplacesLegacyTargetExports(t *testing.T) {
	claims := natsjwt.NewAccountClaims("ASOURCE")
	claims.Exports.Add(
		&natsjwt.Export{Name: sourceSharePrefix + "api-BTARGET", Subject: "$JS.API.CONSUMER.>", Type: natsjwt.Service},
		&natsjwt.Export{Name: sourceSharePrefix + "flow-control-BTARGET", Subject: "$JS.FC.>", Type: natsjwt.Service},
		&natsjwt.Export{Name: sourceDeliveryExportName("BTARGET"), Subject: "$JS.SOURCE.BTARGET.>", Type: natsjwt.Stream},
	)

	normalizeSourceServiceExports(claims)
	ensureSourceServiceExport(claims, &natsjwt.Export{Name: sourceAPIExportName(), Subject: "$JS.API.CONSUMER.>", Type: natsjwt.Service})
	ensureSourceServiceExport(claims, &natsjwt.Export{Name: sourceFlowControlExportName(), Subject: "$JS.FC.>", Type: natsjwt.Service})

	if len(claims.Exports) != 3 {
		t.Fatalf("exports = %#v, want delivery plus two shared service exports", claims.Exports)
	}
	for _, export := range claims.Exports {
		if strings.HasPrefix(export.Name, sourceSharePrefix+"api-") || strings.HasPrefix(export.Name, sourceSharePrefix+"flow-control-") {
			t.Fatalf("legacy export remained: %#v", export)
		}
	}
}

func TestRemoveOrphanedSourceDeliveryExports(t *testing.T) {
	exports := natsjwt.Exports{
		&natsjwt.Export{Name: sourceDeliveryExportName("ACTIVE")},
		&natsjwt.Export{Name: sourceDeliveryExportName("DELETED")},
		&natsjwt.Export{Name: "unmanaged-export"},
	}
	filtered := removeOrphanedSourceDeliveryExports(exports, func(account string) bool {
		return account == "ACTIVE"
	})
	if len(filtered) != 2 || filtered[0].Name != sourceDeliveryExportName("ACTIVE") || filtered[1].Name != "unmanaged-export" {
		t.Fatalf("filtered exports = %#v, want active delivery and unmanaged export", filtered)
	}
}
