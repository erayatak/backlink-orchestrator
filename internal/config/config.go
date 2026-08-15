package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	AppEnv            string
	AppPort           string
	DatabaseURL       string
	PublicBaseURL     string
	SessionSecret     string
	AdminUsername     string
	AdminPassword     string
	BootstrapTokenTTL time.Duration
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	TaskLeaseDuration time.Duration
	TaskMaxAttempts   int
	LogLevel          string
	R2Endpoint        string
	R2AccessKeyID     string
	R2SecretAccess    string
	R2Bucket          string
}

func Load() *Config {
	return &Config{
		AppEnv:            getEnv("APP_ENV", "development"),
		AppPort:           getEnv("APP_PORT", "8080"),
		DatabaseURL:       getEnv("DATABASE_URL", ""),
		PublicBaseURL:     getEnv("PUBLIC_BASE_URL", "http://localhost:8080"),
		SessionSecret:     getEnv("SESSION_SECRET", ""),
		AdminUsername:     getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:     getEnv("ADMIN_PASSWORD_HASH", ""),
		BootstrapTokenTTL: getEnvDuration("BOOTSTRAP_TOKEN_TTL", 15*time.Minute),
		HeartbeatInterval: getEnvDuration("HEARTBEAT_INTERVAL", 30*time.Second),
		HeartbeatTimeout:  getEnvDuration("HEARTBEAT_TIMEOUT", 90*time.Second),
		TaskLeaseDuration: getEnvDuration("TASK_LEASE_DURATION", 10*time.Minute),
		TaskMaxAttempts:   getEnvInt("TASK_MAX_ATTEMPTS", 5),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		R2Endpoint:        getEnv("R2_ENDPOINT", ""),
		R2AccessKeyID:     getEnv("R2_ACCESS_KEY_ID", ""),
		R2SecretAccess:    getEnv("R2_SECRET_ACCESS_KEY", ""),
		R2Bucket:          getEnv("R2_BUCKET", ""),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
		slog.Warn("Invalid integer for env var, using fallback", "key", key, "fallback", fallback)
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
		slog.Warn("Invalid duration for env var, using fallback", "key", key, "fallback", fallback)
	}
	return fallback
}
