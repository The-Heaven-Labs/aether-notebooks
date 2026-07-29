//go:generate swag init -g cmd/aether-server/main.go -o internal/api/docs
package main

// @title Aether API
// @version 1.0.0
// @description Aether Notebooks API — collaborative SQL/data notebook platform
// @host localhost:8088
// @BasePath /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter "Bearer {token}"

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/cache"
	"github.com/the-heaven-labs/aether/internal/config"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/scheduler"
	"github.com/the-heaven-labs/aether/internal/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func usage() {
	fmt.Fprintf(os.Stderr, `Aether Notebooks Server

Usage: aether-server [options]

Options:
  -h, --help         Show this help message and exit
  --migrate-only     Only run database migrations and exit (for pipeline migration jobs)

Configuration is done via environment variables:

  Required (unless --migrate-only):
    AETHER_MASTER_KEY           AES key for encrypting connector credentials (32+ chars)
    AETHER_JWT_SECRET           Secret used to sign JWT tokens

  Database:
    AETHER_DATABASE_URL         Postgres connection URL (default: constructed from individual vars below)
    AETHER_DATABASE_HOST        Postgres host (default: "localhost") — used when AETHER_DATABASE_URL is empty
    AETHER_DATABASE_PORT        Postgres port (default: "5432")
    AETHER_DATABASE_NAME        Postgres database name (default: "aether")
    AETHER_DATABASE_USER_ENV    Env var name containing the Postgres user (e.g. "DB_USER_AETHER_NOTEBOOKS")
    AETHER_DATABASE_PASSWORD_ENV Env var name containing the Postgres password (e.g. "DB_PASS_AETHER_NOTEBOOKS")
    AETHER_DATABASE_SSLMODE     Postgres SSL mode (default: "disable")
    AETHER_DATABASE_SCHEMA      PostgreSQL schema / search_path (default: "aether_notebooks") — applied via SET after connecting
    AETHER_DISABLE_MIGRATIONS   Skip embedded migrations on startup (default: "false") — set to "true" when pipeline handles migrations
    AETHER_REDIS_URL            Redis connection URL (default: "redis://localhost:6379")

  Server:
	AETHER_PORT                 HTTP listen port (default: "8088")
	AETHER_PUBLIC_URL           Public-facing URL for link generation (default: "http://localhost:8088")
    AETHER_FRONTEND_URL         Frontend URL for CORS and OIDC redirect (default value of AETHER_PUBLIC_URL)
    AETHER_DISABLE_REGISTRATION Disable new user registration (default: "false")

  Storage:
    AETHER_STORAGE_BACKEND      Storage backend: "local" or "s3" (default: "local")
    AETHER_ATTACHMENT_DIR       Local attachment directory (default: "./attachments")
    AETHER_MAX_ATTACHMENT_BYTES Max attachment file size in bytes (default: "10485760")
    AETHER_S3_ENDPOINT          S3-compatible endpoint URL
    AETHER_S3_BUCKET            S3 bucket name
    AETHER_S3_REGION            S3 region (default: "us-east-1")
    AETHER_S3_ACCESS_KEY        S3 access key (optional — when omitted, uses IAM role / default credential chain)
    AETHER_S3_SECRET_KEY        S3 secret key (optional — when omitted, uses IAM role / default credential chain)

  Admin:
    AETHER_PLATFORM_ADMIN_EMAIL Email of user to auto-promote to platform admin

  OIDC / SSO:
    AETHER_OIDC_HOST_REWRITE    "from=to" pair to rewrite the OIDC discovery host

  Frontend Runtime Config:
    AETHER_API_URL              Base URL for REST API from the SPA (default: same-origin)
    AETHER_RELAY_URL            WebSocket URL of the Hocuspocus relay (default: derived from origin as wss://<origin>/relay)

  Dev:
    AETHER_ENABLE_DEV_SEED      Enable dev-only seeding (SSO provider, audit S3 config) (default: "false")

  Webhooks:
    AETHER_TOOL_ALLOWED_DOMAINS Comma-separated list of allowed domains for webhook tools

`)
}

func init() {
	flag.Usage = usage
}

func main() {
	showHelp := flag.Bool("help", false, "Show help and exit")
	migrateOnly := flag.Bool("migrate-only", false, "Only run database migrations and exit")
	flag.Parse()
	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	logLevel := slog.LevelInfo
	switch strings.ToLower(os.Getenv("AETHER_LOG_LEVEL")) {
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	case "debug":
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))

	var cfg *config.Config
	var err error
	if *migrateOnly {
		cfg, err = config.LoadMigrateOnly()
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		slog.Error("config load failed", "error", err, "hint", "run 'aether-server --help' for configuration options")
		os.Exit(1)
	}

	ctx := context.Background()

	// Connect to Postgres
	db, err := database.Connect(ctx, cfg.DatabaseURL, cfg.DatabaseSchema)
	if err != nil {
		slog.Error("database connect failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// --migrate-only: always run migrations and exit (for pipeline migration jobs)
	if *migrateOnly {
		if err := db.Migrate(ctx); err != nil {
			slog.Error("database migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("migrate-only: migrations applied, exiting")
		return
	}

	// Normal startup — only migrate if not disabled
	if !cfg.DisableMigrations {
		if err := db.Migrate(ctx); err != nil {
			slog.Error("database migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("migrations applied")
	} else {
		slog.Info("migrations disabled by config")
	}

	// Connect to Redis
	redisCache, err := cache.New(cfg.RedisURL)
	if err != nil {
		slog.Error("cache connect failed", "error", err)
		os.Exit(1)
	}
	defer redisCache.Close()
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := redisCache.Ping(pingCtx); err != nil {
		slog.Error("cache ping failed", "error", err)
		os.Exit(1)
	}
	slog.Info("redis connected")

	// Seed platform admin from env if configured
	if cfg.PlatformAdminEmail != "" {
		promoted, err := api.SeedPlatformAdmin(ctx, db.Pool, cfg.PlatformAdminEmail)
		if err != nil {
			slog.Warn("failed to seed platform admin", "error", err)
		} else if promoted {
			slog.Info("platform admin seeded", "email", cfg.PlatformAdminEmail)
		} else {
			slog.Info("platform admin email configured but user not found; will take effect after first registration", "email", cfg.PlatformAdminEmail)
		}
	}

	// Initialize services
	jwtIssuer := auth.NewJWTIssuer(cfg.JWTSecret, 24*time.Hour)
	masterKey := crypto.DeriveKey(cfg.MasterKey)
	slog.Info("master key derived", "sha256_first8", fmt.Sprintf("%x", masterKey[:8]))

	// Validate master key works by encrypting/decrypting a known value
	testPlaintext := []byte("master-key-validation")
	testCiphertext, err := crypto.Encrypt(testPlaintext, masterKey)
	if err != nil {
		slog.Error("master key validation: encryption failed", "error", err)
		os.Exit(1)
	}
	decrypted, err := crypto.Decrypt(testCiphertext, masterKey)
	if err != nil || string(decrypted) != string(testPlaintext) {
		slog.Error("master key validation: decryption failed — AETHER_MASTER_KEY may have changed since server start")
		os.Exit(1)
	}

	// Seed dev SSO providers (Keycloak) and audit S3 config (Garage) if enabled
	if os.Getenv("AETHER_ENABLE_DEV_SEED") == "true" {
		api.SeedDevSSOProviders(ctx, db.Pool, masterKey)
		api.SeedDevAuditS3Config(ctx, db.Pool)
	}

	// Resolve model config env var references
	resolveModelConfigEnvVars(ctx, db.Pool, masterKey)

	auditLogger := audit.NewLogger(db)

	// Start scheduler (runs due notebook schedules every minute)
	sched := scheduler.New(db, func(ctx context.Context, notebookID string, params map[string]string) error {
		slog.Info("scheduler: running notebook", "notebook_id", notebookID)
		return nil
	})
	sched.Start()
	defer sched.Stop()

	// Build HTTP server
	srv := api.NewServer(db, jwtIssuer, auditLogger, masterKey, redisCache)

	// Configure storage backend
	var store storage.Storage
	switch cfg.StorageBackend {
	case "s3":
		s3Store, err := storage.NewS3Storage(storage.S3Config{
			Endpoint:  cfg.S3Endpoint,
			Region:    cfg.S3Region,
			Bucket:    cfg.S3Bucket,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
		})
		if err != nil {
			slog.Error("s3 storage init failed", "error", err)
			os.Exit(1)
		}
		store = s3Store
		slog.Info("storage backend: s3", "bucket", cfg.S3Bucket, "endpoint", cfg.S3Endpoint)
	default:
		if cfg.StorageBackend != "local" && cfg.StorageBackend != "" {
			slog.Warn("unknown storage backend, falling back to local", "configured", cfg.StorageBackend)
		}
		localStore, err := storage.NewLocalStorage(cfg.AttachmentDir)
		if err != nil {
			slog.Error("local storage init failed", "error", err)
			os.Exit(1)
		}
		store = localStore
		slog.Info("storage backend: local", "dir", cfg.AttachmentDir)
	}
	srv.SetStorage(store)
	srv.SetAgentStore(store)
	srv.StartBackgroundJobs(ctx)
	srv.StartAuditS3Writers(ctx)
	srv.SetPlatformAdminEmail(cfg.PlatformAdminEmail)
	srv.SetPublicURL(cfg.PublicURL)
	srv.SetFrontendURL(cfg.FrontendURL)
	srv.SetVersion(version, commit, buildDate)
	srv.SetMaxAttachmentBytes(cfg.MaxAttachmentBytes)
	srv.SetToolAllowedDomains(cfg.ToolAllowedDomains)
	srv.SetOIDCHostRewrite(cfg.OIDCHostRewrite)
	srv.SetDisableRegistration(cfg.DisableRegistration)
	srv.SetFrontendHandler(frontendHandler(&runtimeConfig{
		APIURL:   cfg.APIURL,
		RelayURL: cfg.RelayURL,
	}))
	httpSrv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("aether-server listening", "port", cfg.Port)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen failed", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown failed", "error", err)
		os.Exit(1)
	}
	slog.Info("stopped")
}

// resolveModelConfigEnvVars scans model configs with api_key_env_var set,
// resolves the env var reference, encrypts the value, and updates api_key_encrypted.
// This runs on every startup so that changing the env var and restarting picks up the new value.
func resolveModelConfigEnvVars(ctx context.Context, pool *pgxpool.Pool, masterKey []byte) {
	rows, err := pool.Query(ctx, `SELECT id, api_key_env_var FROM model_configs WHERE api_key_env_var IS NOT NULL`)
	if err != nil {
		slog.Warn("resolve model config env vars: query failed", "error", err)
		return
	}
	defer rows.Close()

	type row struct {
		id     string
		envVar string
	}
	var toResolve []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.envVar); err != nil {
			slog.Warn("resolve model config env vars: scan failed", "error", err)
			continue
		}
		toResolve = append(toResolve, r)
	}

	resolved := 0
	skipped := 0
	for _, r := range toResolve {
		val := os.Getenv(r.envVar)
		if val == "" {
			slog.Warn("resolve model config env vars: env var is empty or unset",
				"model_config_id", r.id, "env_var", r.envVar)
			skipped++
			continue
		}
		encrypted, err := crypto.Encrypt([]byte(val), masterKey)
		if err != nil {
			slog.Warn("resolve model config env vars: encrypt failed",
				"model_config_id", r.id, "env_var", r.envVar, "error", err)
			skipped++
			continue
		}
		_, err = pool.Exec(ctx, `UPDATE model_configs SET api_key_encrypted = $1, updated_at = NOW() WHERE id = $2`, encrypted, r.id)
		if err != nil {
			slog.Warn("resolve model config env vars: update failed",
				"model_config_id", r.id, "error", err)
			skipped++
			continue
		}
		resolved++
	}

	if resolved > 0 || skipped > 0 {
		slog.Info("model config env vars resolved",
			"resolved", resolved, "skipped", skipped)
	}
}
