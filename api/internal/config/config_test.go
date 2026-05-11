package config

import "testing"

func TestConstants(t *testing.T) {
	if APIPort != "9322" {
		t.Fatalf("unexpected APIPort: %q", APIPort)
	}
	if DBPath != "/app/data/console.db" {
		t.Fatalf("unexpected DBPath: %q", DBPath)
	}
}

func TestLoadUsesEnvironmentValues(t *testing.T) {
	t.Setenv("NATS_URL", "nats://example:4222")
	t.Setenv("NATS_SYS_NKEY", "SYS-SEED")
	t.Setenv("OPERATOR_NKEY", "OP-SEED")
	t.Setenv("ALLOWED_ORIGINS", "https://app.example")
	t.Setenv("ADMIN_ID", "admin")
	t.Setenv("ADMIN_PASSWORD", "secret")

	cfg := Load()
	if cfg.NATSURL != "nats://example:4222" {
		t.Fatalf("unexpected NATSURL: %q", cfg.NATSURL)
	}
	if cfg.NATSSysNKey != "SYS-SEED" {
		t.Fatalf("unexpected NATSSysNKey: %q", cfg.NATSSysNKey)
	}
	if cfg.OperatorNKey != "OP-SEED" {
		t.Fatalf("unexpected OperatorNKey: %q", cfg.OperatorNKey)
	}
	if cfg.AllowedOrigins != "https://app.example" {
		t.Fatalf("unexpected AllowedOrigins: %q", cfg.AllowedOrigins)
	}
	if cfg.AdminID != "admin" {
		t.Fatalf("unexpected AdminID: %q", cfg.AdminID)
	}
	if cfg.AdminPassword != "secret" {
		t.Fatalf("unexpected AdminPassword: %q", cfg.AdminPassword)
	}
}

func TestLoadUsesFallbacksForMissingOrEmptyValues(t *testing.T) {
	t.Setenv("NATS_URL", "")
	t.Setenv("NATS_SYS_NKEY", "")
	t.Setenv("OPERATOR_NKEY", "")
	t.Setenv("ALLOWED_ORIGINS", "")
	t.Setenv("ADMIN_ID", "")
	t.Setenv("ADMIN_PASSWORD", "")

	cfg := Load()
	if cfg.NATSURL != "" {
		t.Fatalf("unexpected NATSURL: %q", cfg.NATSURL)
	}
	if cfg.NATSSysNKey != "" {
		t.Fatalf("unexpected NATSSysNKey: %q", cfg.NATSSysNKey)
	}
	if cfg.OperatorNKey != "" {
		t.Fatalf("unexpected OperatorNKey: %q", cfg.OperatorNKey)
	}
	if cfg.AllowedOrigins != "*" {
		t.Fatalf("unexpected AllowedOrigins: %q", cfg.AllowedOrigins)
	}
	if cfg.AdminID != "" {
		t.Fatalf("unexpected AdminID: %q", cfg.AdminID)
	}
	if cfg.AdminPassword != "" {
		t.Fatalf("unexpected AdminPassword: %q", cfg.AdminPassword)
	}
}
