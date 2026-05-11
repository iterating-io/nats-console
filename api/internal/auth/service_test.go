package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssueAndParse(t *testing.T) {
	svc := NewService("test-secret")

	token, err := svc.Issue("alice", "admin")
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	claims, err := svc.Parse(token)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if claims.Username != "alice" {
		t.Fatalf("unexpected username: %q", claims.Username)
	}
	if claims.Role != "admin" {
		t.Fatalf("unexpected role: %q", claims.Role)
	}
	if claims.Issuer != "nats-console-api" {
		t.Fatalf("unexpected issuer: %q", claims.Issuer)
	}
	if claims.ExpiresAt == nil || claims.IssuedAt == nil {
		t.Fatal("expected issued-at and expires-at to be set")
	}
	if got := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time); got != 12*time.Hour {
		t.Fatalf("unexpected ttl: %s", got)
	}
}

func TestParseRejectsWrongAlgorithm(t *testing.T) {
	svc := NewService("test-secret")

	claims := &Claims{Username: "alice", Role: "admin"}
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	raw, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("SignedString failed: %v", err)
	}

	if _, err := svc.Parse(raw); err == nil || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("expected invalid token error, got %v", err)
	}
}

func TestParseRejectsInvalidToken(t *testing.T) {
	svc := NewService("test-secret")

	if _, err := svc.Parse("not-a-token"); err == nil {
		t.Fatal("expected error for invalid token")
	}
}
