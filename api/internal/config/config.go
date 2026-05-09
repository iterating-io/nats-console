package config

import "os"

// Config holds runtime settings for the API service.
type Config struct {
	Port           string
	JWTSecret      string
	NATSURL        string
	NATSSysNKey    string
	JSAccountName  string
	OperatorNKey   string
	AllowedOrigins string
	DBPath         string
}

// Load reads configuration from environment variables with safe defaults for local development.
func Load() Config {
	cfg := Config{
		Port:           getEnv("API_PORT", "8080"),
		JWTSecret:      getEnv("JWT_SECRET", "change-me-local-secret"),
		NATSURL:        getEnv("NATS_URL", "nats://localhost:4222"),
		NATSSysNKey:    getEnv("NATS_SYS_NKEY", ""),
		JSAccountName:  getEnv("JS_ACCOUNT_NAME", "CONSOLE_JS"),
		OperatorNKey:   getEnv("OPERATOR_NKEY", ""),
		AllowedOrigins: getEnv("ALLOWED_ORIGINS", "http://localhost:5173"),
		DBPath:         getEnv("DB_PATH", "./data/console.db"),
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
