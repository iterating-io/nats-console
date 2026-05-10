package config

import "os"

// APIPort is the hardcoded API server port.
const APIPort = "9322"

// DBPath is the hardcoded SQLite database path.
const DBPath = "/app/data/console.db"

// Config holds runtime settings for the API service.
type Config struct {
	NATSURL        string
	NATSSysNKey    string
	OperatorNKey   string
	AllowedOrigins string
	AdminID        string
	AdminPassword  string
}

// Load reads configuration from environment variables.
func Load() Config {
	cfg := Config{
		NATSURL:        getEnv("NATS_URL", ""),
		NATSSysNKey:    getEnv("NATS_SYS_NKEY", ""),
		OperatorNKey:   getEnv("OPERATOR_NKEY", ""),
		AllowedOrigins: getEnv("ALLOWED_ORIGINS", "*"),
		AdminID:        getEnv("ADMIN_ID", ""),
		AdminPassword:  getEnv("ADMIN_PASSWORD", ""),
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
