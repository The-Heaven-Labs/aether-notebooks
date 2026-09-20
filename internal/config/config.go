// Package config provides environment-based configuration loading for the Aether server.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                       string
	DatabaseURL                string
	DatabaseHost               string
	DatabasePort               string
	DatabaseName               string
	DatabaseUser               string
	DatabasePassword           string
	DatabaseSSMode             string
	DatabaseSchema             string // PostgreSQL search_path
	RedisURL                   string
	MasterKey                  string // for encrypting connector credentials
	JWTSecret                  string
	AttachmentDir              string
	PlatformAdminEmail         string
	PublicURL                  string // base URL used in OAuth callbacks (e.g. https://app.example.com)
	FrontendURL                string // base URL for post-auth redirects; defaults to same host as API
	StorageBackend             string // "local" (default) or "s3"
	S3Endpoint                 string // leave empty for AWS; set for Garage/self-hosted
	S3Bucket                   string
	S3Region                   string
	S3AccessKey                string
	S3SecretKey                string
	MaxAttachmentBytes         int64
	ToolAllowedDomains         []string      // comma-separated domains allowed for webhook tools (bypasses private IP block)
	OIDCHostRewrite            string        // "from=to" pair for rewriting OIDC discovery host (e.g. "localhost:5557=host.docker.internal:5557")
	APIURL                     string        // base URL for REST API from the SPA (empty = same-origin)
	RelayURL                   string        // WebSocket URL of the Hocuspocus relay (empty = derive from origin)
	DisableRegistration        bool          // when true, new users cannot register via email/password (SSO only)
	DisableMigrations          bool          // when true, skip embedded migrations on startup (used when pipeline handles migrations)
	StatsRollupInterval        time.Duration // agent stats rollup cadence (AETHER_AGENT_STATS_ROLLUP_INTERVAL, default 1h, floor 5m)
	AgentToolTimeoutDefault    time.Duration // fallback agent tool execution budget (AETHER_AGENT_TOOL_TIMEOUT_DEFAULT, default 120s, floor 1s)
	OutputLimitsMaxBytes       int64         // platform ceiling for org-configured output byte caps (AETHER_OUTPUT_LIMITS_MAX_BYTES, default 64MB)
	WarehouseReconcileInterval time.Duration // warehouse ClickHouse reconcile catch-up cadence (AETHER_CH_RECONCILE_INTERVAL, default 10m, floor 1m)
}

func parseCommaList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func resolveEnvRef(key string) string {
	v := os.Getenv(key)
	if v == "" {
		return ""
	}
	return os.Getenv(v)
}

func Load() (*Config, error) {
	return load(false)
}

// LoadMigrateOnly loads configuration for migration-only mode.
// Skips validation of secrets (MasterKey, JWTSecret) since they aren't needed
// to connect to the database and run migrations.
func LoadMigrateOnly() (*Config, error) {
	return load(true)
}

func load(migrateOnly bool) (*Config, error) {
	maxAttachmentBytes, err := strconv.ParseInt(envOrDefault("AETHER_MAX_ATTACHMENT_BYTES", "10485760"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid AETHER_MAX_ATTACHMENT_BYTES: %w", err)
	}
	statsRollupInterval, err := parseStatsRollupInterval(os.Getenv("AETHER_AGENT_STATS_ROLLUP_INTERVAL"))
	if err != nil {
		return nil, err
	}
	agentToolTimeoutDefault, err := parseAgentToolTimeoutDefault(os.Getenv("AETHER_AGENT_TOOL_TIMEOUT_DEFAULT"))
	if err != nil {
		return nil, err
	}
	outputLimitsMaxBytes, err := strconv.ParseInt(envOrDefault("AETHER_OUTPUT_LIMITS_MAX_BYTES", "67108864"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid AETHER_OUTPUT_LIMITS_MAX_BYTES: %w", err)
	}
	if outputLimitsMaxBytes < 0 {
		return nil, fmt.Errorf("AETHER_OUTPUT_LIMITS_MAX_BYTES must be non-negative")
	}
	warehouseReconcileInterval, err := parseWarehouseReconcileInterval(os.Getenv("AETHER_CH_RECONCILE_INTERVAL"))
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Port:                       envOrDefault("AETHER_PORT", "8088"),
		DatabaseURL:                os.Getenv("AETHER_DATABASE_URL"),
		DatabaseHost:               os.Getenv("AETHER_DATABASE_HOST"),
		DatabasePort:               os.Getenv("AETHER_DATABASE_PORT"),
		DatabaseName:               os.Getenv("AETHER_DATABASE_NAME"),
		DatabaseUser:               resolveEnvRef("AETHER_DATABASE_USER_ENV"),
		DatabasePassword:           resolveEnvRef("AETHER_DATABASE_PASSWORD_ENV"),
		DatabaseSSMode:             envOrDefault("AETHER_DATABASE_SSLMODE", "disable"),
		DatabaseSchema:             envOrDefault("AETHER_DATABASE_SCHEMA", "aether_notebooks"),
		RedisURL:                   envOrDefault("AETHER_REDIS_URL", "redis://localhost:6379"),
		MasterKey:                  os.Getenv("AETHER_MASTER_KEY"),
		JWTSecret:                  os.Getenv("AETHER_JWT_SECRET"),
		AttachmentDir:              envOrDefault("AETHER_ATTACHMENT_DIR", "./attachments"),
		PlatformAdminEmail:         os.Getenv("AETHER_PLATFORM_ADMIN_EMAIL"),
		PublicURL:                  os.Getenv("AETHER_PUBLIC_URL"),
		FrontendURL:                os.Getenv("AETHER_FRONTEND_URL"),
		StorageBackend:             envOrDefault("AETHER_STORAGE_BACKEND", "local"),
		S3Endpoint:                 os.Getenv("AETHER_S3_ENDPOINT"),
		S3Bucket:                   os.Getenv("AETHER_S3_BUCKET"),
		S3Region:                   envOrDefault("AETHER_S3_REGION", "us-east-1"),
		S3AccessKey:                os.Getenv("AETHER_S3_ACCESS_KEY"),
		S3SecretKey:                os.Getenv("AETHER_S3_SECRET_KEY"),
		MaxAttachmentBytes:         maxAttachmentBytes,
		ToolAllowedDomains:         parseCommaList(os.Getenv("AETHER_TOOL_ALLOWED_DOMAINS")),
		OIDCHostRewrite:            os.Getenv("AETHER_OIDC_HOST_REWRITE"),
		APIURL:                     os.Getenv("AETHER_API_URL"),
		RelayURL:                   os.Getenv("AETHER_RELAY_URL"),
		DisableRegistration:        envOrDefault("AETHER_DISABLE_REGISTRATION", "false") == "true",
		DisableMigrations:          envOrDefault("AETHER_DISABLE_MIGRATIONS", "false") == "true",
		StatsRollupInterval:        statsRollupInterval,
		AgentToolTimeoutDefault:    agentToolTimeoutDefault,
		OutputLimitsMaxBytes:       outputLimitsMaxBytes,
		WarehouseReconcileInterval: warehouseReconcileInterval,
	}

	// If no explicit DatabaseURL, build from individual components.
	if cfg.DatabaseURL == "" {
		host := cfg.DatabaseHost
		if host == "" {
			host = "localhost"
		}
		port := cfg.DatabasePort
		if port == "" {
			port = "5432"
		}
		name := cfg.DatabaseName
		if name == "" {
			name = "aether"
		}
		user := cfg.DatabaseUser
		if user == "" {
			user = "aether"
		}
		password := cfg.DatabasePassword
		if password == "" {
			password = "aether_dev"
		}
		cfg.DatabaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, name, cfg.DatabaseSSMode)
	}

	if !migrateOnly {
		if cfg.MasterKey == "" {
			return nil, fmt.Errorf("AETHER_MASTER_KEY is required — set this environment variable to a secret value (32+ characters)")
		}
		if cfg.JWTSecret == "" {
			return nil, fmt.Errorf("AETHER_JWT_SECRET is required — set this environment variable to a secret value for signing JWT tokens")
		}
	}
	if cfg.StorageBackend == "s3" {
		if cfg.S3Bucket == "" {
			return nil, fmt.Errorf("AETHER_S3_BUCKET is required when AETHER_STORAGE_BACKEND=s3")
		}
		// Access key and secret are optional — when both are empty,
		// the S3 client uses the default AWS credential chain (IRSA, env vars, etc.)
	}
	if cfg.Port == "" {
		cfg.Port = "8088"
	}
	return cfg, nil
}

// parseStatsRollupInterval parses AETHER_AGENT_STATS_ROLLUP_INTERVAL as a Go
// duration. Empty means the 1h default; values below the 5m floor are raised
// to it so a misconfigured env var cannot hot-loop the rollup.
func parseStatsRollupInterval(raw string) (time.Duration, error) {
	if raw == "" {
		return time.Hour, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid AETHER_AGENT_STATS_ROLLUP_INTERVAL %q: %w", raw, err)
	}
	if d < 5*time.Minute {
		return 5 * time.Minute, nil
	}
	return d, nil
}

// parseAgentToolTimeoutDefault parses AETHER_AGENT_TOOL_TIMEOUT_DEFAULT as a
// Go duration. Empty means the 120s default; values below the 1s floor are
// raised to it so a misconfigured env var cannot create an unusable budget.
func parseAgentToolTimeoutDefault(raw string) (time.Duration, error) {
	if raw == "" {
		return 120 * time.Second, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid AETHER_AGENT_TOOL_TIMEOUT_DEFAULT %q: %w", raw, err)
	}
	if d < time.Second {
		return time.Second, nil
	}
	return d, nil
}

// DefaultWarehouseReconcileInterval is the catch-up cadence used when
// AETHER_CH_RECONCILE_INTERVAL is unset. Server construction falls back to it
// so the value has a single source.
const DefaultWarehouseReconcileInterval = 10 * time.Minute

// parseWarehouseReconcileInterval parses AETHER_CH_RECONCILE_INTERVAL as a Go
// duration. Empty means the default; values below the 1m floor are raised to
// it so a misconfigured env var cannot hot-loop the reconcile catch-up.
func parseWarehouseReconcileInterval(raw string) (time.Duration, error) {
	if raw == "" {
		return DefaultWarehouseReconcileInterval, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid AETHER_CH_RECONCILE_INTERVAL %q: %w", raw, err)
	}
	if d < time.Minute {
		return time.Minute, nil
	}
	return d, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// ResolveOutputLimit returns the effective byte cap for an org-configured value
// given the platform ceiling. orgValue <= 0 means unlimited (explicit per-org
// opt-out, passed through unchanged). Otherwise the value is clamped to the
// platform ceiling so a single org admin cannot tune around the cap that
// protects shared pods. A platformMax <= 0 means no ceiling is configured.
func ResolveOutputLimit(orgValue, platformMax int64) int64 {
	if orgValue <= 0 {
		return 0
	}
	if platformMax > 0 && orgValue > platformMax {
		return platformMax
	}
	return orgValue
}
