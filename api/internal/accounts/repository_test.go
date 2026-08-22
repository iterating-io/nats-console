package accounts

import "testing"

func TestUpsertLoadedRefreshesSourceEnabled(t *testing.T) {
	repo := NewRepository()
	repo.AddAccount(Record{
		Name:          "A",
		Operator:      "default",
		PublicKey:     "AOLD",
		JSEnabled:     true,
		SourceEnabled: false,
	})

	repo.UpsertLoaded([]Record{{
		Name:          "A",
		Operator:      "default",
		PublicKey:     "ANEW",
		JSEnabled:     true,
		SourceEnabled: true,
	}})

	account, ok := repo.FindByName("default", "A")
	if !ok {
		t.Fatal("account not found")
	}
	if !account.SourceEnabled {
		t.Fatal("SourceEnabled = false, want true after reload")
	}
	if account.PublicKey != "ANEW" {
		t.Fatalf("PublicKey = %q, want ANEW after reload", account.PublicKey)
	}
}
