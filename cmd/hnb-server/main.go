package main

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
		if _, err := db.Pool.Exec(ctx,
			`UPDATE users SET is_platform_admin=true WHERE email=$1`,
			cfg.PlatformAdminEmail,
		); err != nil {
			log.Printf("warning: failed to seed platform admin: %v", err)
		} else {
			log.Printf("platform admin seeded for %s", cfg.PlatformAdminEmail)
		}
	}

	// Initialize services
	jwtIssuer := auth.NewJWTIssuer(cfg.JWTSecret, 24*time.Hour)
	masterKey := crypto.DeriveKey(cfg.MasterKey)
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
	srv.SetAttachmentDir(cfg.AttachmentDir)
	srv.SetPlatformAdminEmail(cfg.PlatformAdminEmail)
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
