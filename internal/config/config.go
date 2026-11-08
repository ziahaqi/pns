package config

import (
	"errors"
	"os"
)

// Config holds application runtime configuration sourced from environment variables.
type Config struct {
	AppPort       string
	DatabaseURL   string
	ServerKey     string
	SessionSecret string
	// FCMServerKey authorizes calls to Firebase Cloud Messaging. It is kept
	// apart from ServerKey on purpose: ServerKey ships inside the mobile app
	// to call the device API, so anyone can extract it, and it must not also
	// be able to send pushes to every user. Empty means pushes are not sent.
	FCMServerKey string
}

// Load reads environment variables and fills Config with sane defaults.
// Secrets have no defaults: a known fallback would let anyone sign a valid
// admin session cookie.
func Load() (Config, error) {
	cfg := Config{
		AppPort:       getEnv("APP_PORT", "8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://pns:pns@localhost:5432/pns?sslmode=disable"),
		ServerKey:     os.Getenv("SERVER_KEY"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		FCMServerKey:  os.Getenv("FCM_SERVER_KEY"),
	}

	if cfg.ServerKey == "" {
		return Config{}, errors.New("SERVER_KEY must be set to secure device API access")
	}
	if cfg.SessionSecret == "" {
		return Config{}, errors.New("SESSION_SECRET must be set: it signs the admin session cookie")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
