package accounts

import (
	"testing"

	natsjwt "github.com/nats-io/jwt/v2"
)

func TestRemoveManagedSourceShareRemovesFlowControlRoute(t *testing.T) {
	sourceKey, targetKey := "ASOURCE", "BTARGET"
	source := natsjwt.NewAccountClaims(sourceKey)
	target := natsjwt.NewAccountClaims(targetKey)
	source.Exports.Add(
		&natsjwt.Export{Name: sourceAPIExportName(targetKey)},
		&natsjwt.Export{Name: sourceDeliveryExportName(targetKey)},
		&natsjwt.Export{Name: sourceFlowControlExportName(targetKey)},
		&natsjwt.Export{Name: "unmanaged-export"},
	)
	target.Imports.Add(
		&natsjwt.Import{Name: sourceAPIImportName(sourceKey)},
		&natsjwt.Import{Name: sourceDeliveryImportName(sourceKey)},
		&natsjwt.Import{Name: sourceFlowControlImportName(sourceKey)},
		&natsjwt.Import{Name: "unmanaged-import"},
	)

	removeManagedSourceShare(source, target, sourceKey, targetKey)

	if len(source.Exports) != 1 || source.Exports[0].Name != "unmanaged-export" {
		t.Fatalf("source exports = %#v, want only unmanaged export", source.Exports)
	}
	if len(target.Imports) != 1 || target.Imports[0].Name != "unmanaged-import" {
		t.Fatalf("target imports = %#v, want only unmanaged import", target.Imports)
	}
}
