package users

import (
	"strings"
	"testing"

	"github.com/nats-io/nkeys"
)

func TestBuildUserCreds(t *testing.T) {
	accountKP, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatalf("create account key pair: %v", err)
	}
	accountSeed, err := accountKP.Seed()
	if err != nil {
		t.Fatalf("export account seed: %v", err)
	}
	accountPub, err := accountKP.PublicKey()
	if err != nil {
		t.Fatalf("export account public key: %v", err)
	}
	userKP, err := nkeys.CreateUser()
	if err != nil {
		t.Fatalf("create user key pair: %v", err)
	}
	userSeed, err := userKP.Seed()
	if err != nil {
		t.Fatalf("export user seed: %v", err)
	}
	userPub, err := userKP.PublicKey()
	if err != nil {
		t.Fatalf("export user public key: %v", err)
	}
	creds, err := buildUserCreds(
		"test-user",
		userPub,
		string(userSeed),
		string(accountSeed),
		accountPub,
		[]string{"$JS.API.STREAM.INFO.*"},
		[]string{"$JS.API.CONSUMER.>"},
		[]string{"_INBOX.>"},
	)
	if err != nil {
		t.Fatalf("buildUserCreds failed: %v", err)
	}
	if !strings.Contains(creds, string(userSeed)) {
		t.Fatalf("credentials output does not contain user seed")
	}
	if !strings.Contains(creds, "BEGIN NATS USER JWT") || !strings.Contains(creds, "BEGIN USER NKEY SEED") {
		t.Fatalf("credentials output format looks wrong")
	}
}
