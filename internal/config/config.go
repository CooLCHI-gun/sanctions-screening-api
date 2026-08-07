package config

import (
	"os"
	"strconv"
)

// Config holds all configuration for the application,
// populated from environment variables with sensible defaults.
type Config struct {
	Port     int
	AppEnv   string
	LogLevel string
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		Port:     getEnvInt("PORT", 8080),
		AppEnv:   getEnv("APP_ENV", "development"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
