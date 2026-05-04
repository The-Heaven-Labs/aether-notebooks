package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port               string
	DatabaseURL        string
	RedisURL           string
	MasterKey          string // for encrypting connector credentials
	JWTSecret          string
	AttachmentDir      string
	PlatformAdminEmail string
	PublicURL          string // base URL used in OAuth callbacks (e.g. https://app.example.com)
	FrontendURL        string // base URL for post-auth redirects; defaults to same host as API
	StorageBackend     string // "local" (default) or "s3"
	S3Endpoint         string // leave empty for AWS; set for Garage/self-hosted
	S3Bucket           string
	S3Region           string
	S3AccessKey        string
	S3SecretKey        string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:               envOrDefault("HNB_PORT", "8080"),
		DatabaseURL:        envOrDefault("HNB_DATABASE_URL", "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"),
		RedisURL:           envOrDefault("HNB_REDIS_URL", "redis://localhost:6379"),
		MasterKey:          os.Getenv("HNB_MASTER_KEY"),
		JWTSecret:          os.Getenv("HNB_JWT_SECRET"),
		AttachmentDir:      envOrDefault("HNB_ATTACHMENT_DIR", "./attachments"),
		PlatformAdminEmail: os.Getenv("HNB_PLATFORM_ADMIN_EMAIL"),
		PublicURL:          os.Getenv("HNB_PUBLIC_URL"),
		FrontendURL:        os.Getenv("HNB_FRONTEND_URL"),
		StorageBackend:     envOrDefault("HNB_STORAGE_BACKEND", "local"),
		S3Endpoint:         os.Getenv("HNB_S3_ENDPOINT"),
		S3Bucket:           os.Getenv("HNB_S3_BUCKET"),
		S3Region:           envOrDefault("HNB_S3_REGION", "us-east-1"),
		S3AccessKey:        os.Getenv("HNB_S3_ACCESS_KEY"),
		S3SecretKey:        os.Getenv("HNB_S3_SECRET_KEY"),
	}
	if cfg.MasterKey == "" {
		return nil, fmt.Errorf("HNB_MASTER_KEY is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("HNB_JWT_SECRET is required")
	}
	if cfg.StorageBackend == "s3" {
		if cfg.S3Bucket == "" {
			return nil, fmt.Errorf("HNB_S3_BUCKET is required when HNB_STORAGE_BACKEND=s3")
		}
		if cfg.S3AccessKey == "" {
			return nil, fmt.Errorf("HNB_S3_ACCESS_KEY is required when HNB_STORAGE_BACKEND=s3")
		}
		if cfg.S3SecretKey == "" {
			return nil, fmt.Errorf("HNB_S3_SECRET_KEY is required when HNB_STORAGE_BACKEND=s3")
		}
	}
	return cfg, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
