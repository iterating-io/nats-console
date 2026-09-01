package accounts

import (
	"testing"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

func TestAsyncAPIAliasUsesAccountNameAsOneSafeSubjectToken(t *testing.T) {
	got := asyncAPIAlias("orders.eu/prod")
	want := "checker.orders_eu_prod.$JS.API.STREAM.INFO.>"
	if got != want {
		t.Fatalf("alias = %q, want %q", got, want)
	}
}

func TestAsyncAPIConsumerAliasUsesAccountNameAsOneSafeSubjectToken(t *testing.T) {
	got := asyncAPIConsumerAlias("orders.eu/prod")
	want := "checker.orders_eu_prod.$JS.API.CONSUMER.INFO.>"
	if got != want {
		t.Fatalf("consumer alias = %q, want %q", got, want)
	}
}

func TestAsyncAPIImportRemovalKeepsUnmanagedClaims(t *testing.T) {
	claims := natsjwt.NewAccountClaims("ACHECKER")
	claims.Imports.Add(
		&natsjwt.Import{Name: asyncAPIImportName("ASOURCE")},
		&natsjwt.Import{Name: asyncAPIConsumerImportName("ASOURCE")},
		&natsjwt.Import{Name: "unmanaged-import"},
	)
	claims.Imports = removeImports(claims.Imports, asyncAPIImportName("ASOURCE"), asyncAPIConsumerImportName("ASOURCE"))
	if len(claims.Imports) != 1 || claims.Imports[0].Name != "unmanaged-import" {
		t.Fatalf("imports = %#v, want only unmanaged import", claims.Imports)
	}

	source := natsjwt.NewAccountClaims("ASOURCE")
	source.Exports.Add(
		&natsjwt.Export{Name: asyncAPIExportName()},
		&natsjwt.Export{Name: asyncAPIConsumerExportName()},
		&natsjwt.Export{Name: "unmanaged-export"},
	)
	source.Exports = removeExports(source.Exports, asyncAPIExportName(), asyncAPIConsumerExportName())
	if len(source.Exports) != 1 || source.Exports[0].Name != "unmanaged-export" {
		t.Fatalf("exports = %#v, want only unmanaged export", source.Exports)
	}
}

func TestCheckerStatusUsesDedicatedPrivateSubjects(t *testing.T) {
	if asyncAPIStatusSubject == asyncAPIStatusAlias {
		t.Fatal("status import must not expose the system-account subject directly")
	}
	if asyncAPIStatusImportName() == asyncAPIStatusExportName {
		t.Fatal("status import and export names must be distinct")
	}
}

func TestCheckerPublishAllowedUsesAccountDefaultAllowAndDeny(t *testing.T) {
	claims := natsjwt.NewAccountClaims("ASERVICE")
	if !checkerPublishAllowed(claims, "events.created") {
		t.Fatal("empty publish allow should be unrestricted")
	}
	claims.Account.DefaultPermissions.Pub.Allow = []string{"events.>"}
	if !checkerPublishAllowed(claims, "events.created") {
		t.Fatal("matching publish allow should pass")
	}
	if checkerPublishAllowed(claims, "commands.run") {
		t.Fatal("subject outside publish allow should fail")
	}
	claims.Account.DefaultPermissions.Pub.Deny = []string{"events.private.>"}
	if checkerPublishAllowed(claims, "events.private.created") {
		t.Fatal("matching publish deny should override allow")
	}
}

func TestSourceSharingStatusRecognizesOnlyTokenProtectedClaims(t *testing.T) {
	source := natsjwt.NewAccountClaims("ASOURCE")
	target := natsjwt.NewAccountClaims("ATARGET")
	deliverySubject := sourceDeliveryPrefix("ATARGET") + ".>"
	source.Exports.Add(
		&natsjwt.Export{Name: sourceAPIExportName(), Subject: "$JS.API.CONSUMER.>", Type: natsjwt.Service, TokenReq: true},
		&natsjwt.Export{Name: sourceDeliveryExportName("ATARGET"), Subject: natsjwt.Subject(deliverySubject), Type: natsjwt.Stream, TokenReq: true},
		&natsjwt.Export{Name: sourceFlowControlExportName(), Subject: "$JS.FC.>", Type: natsjwt.Service, TokenReq: true},
	)
	target.Imports.Add(
		&natsjwt.Import{Name: sourceAPIImportName("ASOURCE"), Account: "ASOURCE", Subject: "$JS.API.CONSUMER.>", Type: natsjwt.Service, Token: "token"},
		&natsjwt.Import{Name: sourceDeliveryImportName("ASOURCE"), Account: "ASOURCE", Subject: natsjwt.Subject(deliverySubject), Type: natsjwt.Stream, Token: "token"},
		&natsjwt.Import{Name: sourceFlowControlImportName("ASOURCE"), Account: "ASOURCE", Subject: "$JS.FC.>", Type: natsjwt.Service, Token: "token"},
	)
	if !hasSourceExport(source.Exports, sourceAPIExportName(), "$JS.API.CONSUMER.>", natsjwt.Service) {
		t.Fatal("expected token-required source export to be recognized")
	}
	if !hasSourceImport(target.Imports, sourceDeliveryImportName("ASOURCE"), "ASOURCE", deliverySubject, natsjwt.Stream) {
		t.Fatal("expected token-protected delivery import to be recognized")
	}
	target.Imports[1].Token = ""
	if hasSourceImport(target.Imports, sourceDeliveryImportName("ASOURCE"), "ASOURCE", deliverySubject, natsjwt.Stream) {
		t.Fatal("import without activation token must not be considered enabled")
	}
}

func TestCheckerConsumerInfoImportCanUseBroadSourceActivation(t *testing.T) {
	sourceKey, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	source, err := sourceKey.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	checkerKey, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	checker, err := checkerKey.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	token, err := sourceActivationToken(checker, source, "$JS.API.CONSUMER.>", natsjwt.Service, sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	claims := natsjwt.NewAccountClaims(checker)
	claims.Imports.Add(&natsjwt.Import{
		Name: "consumer-info", Account: source, Subject: "$JS.API.CONSUMER.INFO.>",
		LocalSubject: "checker.source.$JS.API.CONSUMER.INFO.>", Type: natsjwt.Service, Token: token,
	})
	validation := natsjwt.CreateValidationResults()
	claims.Validate(validation)
	if !validation.IsEmpty() {
		t.Fatalf("narrow Consumer Info import must be contained by broad activation: %v", validation.Issues)
	}
}

func TestCheckerConsumerExportPlanReusesBroadSourceExport(t *testing.T) {
	exports := natsjwt.Exports{
		&natsjwt.Export{Name: sourceAPIExportName(), Subject: "$JS.API.CONSUMER.>", Type: natsjwt.Service, TokenReq: true},
	}
	subject, addNarrow := checkerConsumerExportPlan(exports)
	if subject != "$JS.API.CONSUMER.>" || addNarrow {
		t.Fatalf("plan = (%q, %v), want broad activation without narrow export", subject, addNarrow)
	}

	subject, addNarrow = checkerConsumerExportPlan(nil)
	if subject != "$JS.API.CONSUMER.INFO.>" || !addNarrow {
		t.Fatalf("plan = (%q, %v), want narrow checker export", subject, addNarrow)
	}
}
