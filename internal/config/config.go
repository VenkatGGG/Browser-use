package config

import (
	"os"
	"time"
)

type Config struct {
	HTTPAddr     string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

func Load() Config {
	return Config{
		HTTPAddr:     envOrDefault("ORCHESTRATOR_HTTP_ADDR", ":8080"),
		ReadTimeout:  durationOrDefault("ORCHESTRATOR_READ_TIMEOUT", 15*time.Second),
		WriteTimeout: durationOrDefault("ORCHESTRATOR_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:  durationOrDefault("ORCHESTRATOR_IDLE_TIMEOUT", 60*time.Second),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
