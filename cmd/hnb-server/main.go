package main

// @title hnb API
// @version 1.0.0
// @description Heaven's Notebooks API — collaborative SQL/data notebook platform
// @host localhost:8080
// @BasePath /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter "Bearer {token}"

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/cache"
	"github.com/heavenlabs/hnb/internal/config"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/database"
	"github.com/heavenlabs/hnb/internal/scheduler"
	"github.com/heavenlabs/hnb/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()

	// Connect to Postgres and run migrations
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations applied")

	// Connect to Redis
	redisCache, err := cache.New(cfg.RedisURL)
	if err != nil {
		log.Fatalf("cache: %v", err)
	}
	defer redisCache.Close()
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := redisCache.Ping(pingCtx); err != nil {
		log.Fatalf("cache ping: %v", err)
	}
	log.Println("redis connected")

	// Seed platform admin from env if configured
	if cfg.PlatformAdminEmail != "" {
		promoted, err := api.SeedPlatformAdmin(ctx, db.Pool, cfg.PlatformAdminEmail)
		if err != nil {
			log.Printf("warning: failed to seed platform admin: %v", err)
		} else if promoted {
			log.Printf("platform admin seeded for %s", cfg.PlatformAdminEmail)
		} else {
			log.Printf("platform admin email configured (%s) but user not found; will take effect after first registration", cfg.PlatformAdminEmail)
		}
	}

	// Initialize services
	jwtIssuer := auth.NewJWTIssuer(cfg.JWTSecret, 24*time.Hour)
	masterKey := crypto.DeriveKey(cfg.MasterKey)
	log.Printf("master key: env_value=%q derived_sha256_first8=%x", cfg.MasterKey, masterKey[:8])

	// Validate master key works by encrypting/decrypting a known value
	testPlaintext := []byte("master-key-validation")
	testCiphertext, err := crypto.Encrypt(testPlaintext, masterKey)
	if err != nil {
		log.Fatalf("master key validation: encryption failed: %v", err)
	}
	decrypted, err := crypto.Decrypt(testCiphertext, masterKey)
	if err != nil || string(decrypted) != string(testPlaintext) {
		log.Fatalf("master key validation: decryption failed — HNB_MASTER_KEY may have changed since server start")
	}

	// Seed dev SSO providers (Keycloak) if none exist yet
	api.SeedDevSSOProviders(ctx, db.Pool, masterKey)

	auditLogger := audit.NewLogger(db)

	// Start scheduler (runs due notebook schedules every minute)
	sched := scheduler.New(db, func(ctx context.Context, notebookID string, params map[string]string) error {
		// Scheduled execution is handled via the executor — log only for now
		log.Printf("scheduler: running notebook %s", notebookID)
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
			log.Fatalf("s3 storage: %v", err)
		}
		store = s3Store
		log.Printf("storage: s3 (bucket=%s, endpoint=%q)", cfg.S3Bucket, cfg.S3Endpoint)
	default:
		if cfg.StorageBackend != "local" && cfg.StorageBackend != "" {
			log.Printf("warning: unknown storage backend %q, falling back to local", cfg.StorageBackend)
		}
		localStore, err := storage.NewLocalStorage(cfg.AttachmentDir)
		if err != nil {
			log.Fatalf("local storage: %v", err)
		}
		store = localStore
		log.Printf("storage: local (%s)", cfg.AttachmentDir)
	}
	srv.SetStorage(store)
	srv.StartBackgroundJobs(ctx)
	srv.SetPlatformAdminEmail(cfg.PlatformAdminEmail)
	srv.SetPublicURL(cfg.PublicURL)
	srv.SetFrontendURL(cfg.FrontendURL)
	srv.SetMaxAttachmentBytes(cfg.MaxAttachmentBytes)
	srv.SetToolAllowedDomains(cfg.ToolAllowedDomains)
	srv.SetOIDCHostRewrite(cfg.OIDCHostRewrite)
	srv.SetDisableRegistration(cfg.DisableRegistration)
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
		log.Printf("hnb-server listening on :%s", cfg.Port)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("stopped")
}
