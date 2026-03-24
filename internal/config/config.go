package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
	RedisURL    string
	MasterKey   string // for encrypting connector credentials
	JWTSecret   string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:        envOrDefault("HNB_PORT", "8080"),
		DatabaseURL: envOrDefault("HNB_DATABASE_URL", "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"),
		RedisURL:    envOrDefault("HNB_REDIS_URL", "redis://localhost:6379"),
		MasterKey:   os.Getenv("HNB_MASTER_KEY"),
		JWTSecret:   os.Getenv("HNB_JWT_SECRET"),
	}
	if cfg.MasterKey == "" {
		return nil, fmt.Errorf("HNB_MASTER_KEY is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("HNB_JWT_SECRET is required")
	}
	return cfg, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
