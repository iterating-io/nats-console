package accounts

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nats-io/nkeys"

	"github.com/iterating-io/nats-console/api/internal/config"
)

func TestSystemAccountPublicKey(t *testing.T) {
	kp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatalf("create account key pair: %v", err)
	}
	seed, err := kp.Seed()
	if err != nil {
		t.Fatalf("export seed: %v", err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		t.Fatalf("export public key: %v", err)
	}
	svc := NewService(config.Config{NATSSysNKey: string(seed)}, nil, NewRepository(), nil)
	if got := svc.SystemAccountPublicKey(); got != pub {
		t.Fatalf("unexpected public key: got=%q want=%q", got, pub)
	}

	svc = NewService(config.Config{NATSSysNKey: "BAD-SEED"}, nil, NewRepository(), nil)
	if got := svc.SystemAccountPublicKey(); got != "" {
		t.Fatalf("expected empty key for invalid seed, got=%q", got)
	}
}

func TestMatchSubject(t *testing.T) {
	tests := []struct {
		name    string
		rule    string
		subject string
		want    bool
	}{
		{name: "exact match", rule: "foo.bar", subject: "foo.bar", want: true},
		{name: "exact mismatch", rule: "foo.bar", subject: "foo.baz", want: false},
		{name: "single token wildcard", rule: "foo.*", subject: "foo.bar", want: true},
		{name: "single token wildcard length mismatch", rule: "foo.*", subject: "foo.bar.baz", want: false},
		{name: "multi token wildcard", rule: "foo.>", subject: "foo.bar.baz", want: true},
		{name: "empty rule", rule: "", subject: "foo", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchSubject(tt.rule, tt.subject); got != tt.want {
				t.Fatalf("MatchSubject(%q, %q)=%v, want %v", tt.rule, tt.subject, got, tt.want)
			}
		})
	}
}

func TestUniqueTrimmedSubjects(t *testing.T) {
	got := UniqueTrimmedSubjects([]string{"  foo.bar  ", "foo.bar", "", "   ", "a.b", "a.b", "c.>"})
	want := []string{"foo.bar", "a.b", "c.>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UniqueTrimmedSubjects() mismatch: got=%v want=%v", got, want)
	}
}

func TestSubjectAllowed(t *testing.T) {
	allowed := []string{"events.*", "metrics.>"}
	if !SubjectAllowed("events.created", allowed) {
		t.Fatal("expected events.created to be allowed")
	}
	if SubjectAllowed("logs.error", allowed) {
		t.Fatal("expected logs.error to be denied")
	}
}

func TestValidateClaimUpdateResponse(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		account   string
		wantError bool
		contains  string
	}{
		{name: "success", response: "{\"data\":{\"account\":\"A123\",\"code\":200,\"message\":\"jwt updated\"}}", account: "A123"},
		{name: "structured error", response: "{\"error\":{\"account\":\"A123\",\"code\":500,\"description\":\"jwt validation failed\"}}", account: "A123", wantError: true, contains: "jwt validation failed"},
		{name: "malformed JSON", response: "{", account: "A123", wantError: true, contains: "decode NATS claim update response"},
		{name: "missing result", response: "{}", account: "A123", wantError: true, contains: "neither data nor error"},
		{name: "failure status", response: "{\"data\":{\"account\":\"A123\",\"code\":500,\"message\":\"not applied\"}}", account: "A123", wantError: true, contains: "code 500"},
		{name: "account mismatch", response: "{\"data\":{\"account\":\"A999\",\"code\":200,\"message\":\"jwt updated\"}}", account: "A123", wantError: true, contains: "account mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateClaimUpdateResponse([]byte(test.response), test.account)
			if test.wantError && err == nil {
				t.Fatal("expected error")
			}
			if !test.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.contains != "" && (err == nil || !strings.Contains(err.Error(), test.contains)) {
				t.Fatalf("error = %v, want substring %q", err, test.contains)
			}
		})
	}
}
