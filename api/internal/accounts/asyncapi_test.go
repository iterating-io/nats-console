package accounts

import (
	"testing"

	natsjwt "github.com/nats-io/jwt/v2"
)

func TestAsyncAPIAliasUsesAccountNameAsOneSafeSubjectToken(t *testing.T) {
	got := asyncAPIAlias("orders.eu/prod")
	want := "checker.orders_eu_prod.$JS.API.STREAM.INFO.>"
	if got != want {
		t.Fatalf("alias = %q, want %q", got, want)
	}
}

func TestAsyncAPIImportRemovalKeepsUnmanagedClaims(t *testing.T) {
	claims := natsjwt.NewAccountClaims("ACHECKER")
	claims.Imports.Add(
		&natsjwt.Import{Name: asyncAPIImportName("ASOURCE")},
		&natsjwt.Import{Name: "unmanaged-import"},
	)
	claims.Imports = removeImports(claims.Imports, asyncAPIImportName("ASOURCE"))
	if len(claims.Imports) != 1 || claims.Imports[0].Name != "unmanaged-import" {
		t.Fatalf("imports = %#v, want only unmanaged import", claims.Imports)
	}

	source := natsjwt.NewAccountClaims("ASOURCE")
	source.Exports.Add(
		&natsjwt.Export{Name: asyncAPIExportName()},
		&natsjwt.Export{Name: "unmanaged-export"},
	)
	source.Exports = removeExports(source.Exports, asyncAPIExportName())
	if len(source.Exports) != 1 || source.Exports[0].Name != "unmanaged-export" {
		t.Fatalf("exports = %#v, want only unmanaged export", source.Exports)
	}
}
