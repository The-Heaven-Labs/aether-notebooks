package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/cache"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/config"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/storage"
)

// warehouseSyncer schedules reconciliation of one warehouse's ClickHouse
// access state. *chaccess.SyncService is the production implementation; tests
// inject a recorder.
type warehouseSyncer interface{ Enqueue(uuid.UUID) }

// Server is the HTTP server for the Aether API, holding all dependencies.
type Server struct {
	db                   *database.DB
	jwt                  *auth.JWTIssuer
	audit                *audit.Logger
	masterKey            []byte
	hub                  *Hub
	mux                  *http.ServeMux
	store                storage.Storage
	platformAdminEmail   string
	disableRegistration  bool
	publicURL            string
	frontendURL          string
	Cache                *cache.Cache
	maxAttachmentBytes   int64
	outputLimitsMaxBytes int64 // platform ceiling for org output byte caps (AETHER_OUTPUT_LIMITS_MAX_BYTES)
	agentEngine          *agent.Engine
	upgrader             websocket.Upgrader
	toolAllowedDomains   []string
	sessionCancels       sync.Map                        // sessionID -> context.CancelFunc
	subdomainMW          func(http.Handler) http.Handler // host → org resolution
	oidcRewriteFrom      string                          // host rewrite for OIDC discovery inside Docker (e.g. "localhost:5557")
	oidcRewriteTo        string                          // target host rewrite (e.g. "host.docker.internal:5557")
	frontendHandler      http.Handler                    // embedded web frontend SPA (nil in tests)
	version              string                          // build version (set via ldflags)
	commit               string                          // git commit (set via ldflags)
	buildDate            string                          // build date (set via ldflags)
	warehouseSync        warehouseSyncer                 // debounced ClickHouse access sync (nil disables triggers)
	// chTablePermissions is the AETHER_CH_TABLE_PERMISSIONS kill switch. When
	// false (default), managed connectors execute through the legacy
	// stored-credential path and the warehouse sync worker stays dormant;
	// warehouse CRUD and grants remain usable for staged setup.
	chTablePermissions bool
	// warehouseReconcileInterval is the periodic catch-up cadence for the
	// warehouse sync loop (AETHER_CH_RECONCILE_INTERVAL); <= 0 means the
	// package default.
	warehouseReconcileInterval time.Duration
	warehouseLoop              backgroundLoop // periodic catch-up enqueue loop
	// connPool holds per-user ClickHouse connections leased by warehouse-scoped
	// HTTP executions. CloseAll runs from Close; CloseIdle runs on a ticker
	// owned by connPoolLoop.
	connPool     *executor.ConnPool
	connPoolLoop backgroundLoop
	closeOnce    sync.Once // makes Close idempotent
}

// NewServer creates a new Aether API server with the provided dependencies.
func NewServer(db *database.DB, jwt *auth.JWTIssuer, auditLogger *audit.Logger, masterKey []byte, redisCache *cache.Cache) *Server {
	var rdb *redis.Client
	if redisCache != nil {
		rdb = redisCache.Client()
	}
	s := &Server{
		db:                         db,
		jwt:                        jwt,
		audit:                      auditLogger,
		masterKey:                  masterKey,
		hub:                        NewHub(rdb),
		mux:                        http.NewServeMux(),
		Cache:                      redisCache,
		upgrader:                   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		warehouseReconcileInterval: config.DefaultWarehouseReconcileInterval,
	}
	s.connPool = executor.NewConnPool(executor.PoolConfig{
		MaxPools: connPoolMaxPools,
		IdleTTL:  connPoolIdleTTL,
	})
	s.agentEngine = agent.NewEngine(context.Background(), db.Pool, rdb)
	s.agentEngine.BroadcastFunc = func(notebookID string, msg any) {
		s.hub.Broadcast(notebookID, msg)
	}
	// Agent tool execution resolves warehouse identities and enforces ACLs
	// through the same server methods HTTP uses; the callbacks are wired here
	// because internal/agent cannot import internal/api.
	s.agentEngine.ResolveTarget = s.resolveExecutionTarget
	s.agentEngine.ConnPool = s.connPool
	s.agentEngine.CheckPermissionFunc = s.checkPermission
	// Running-state/cancel lifecycle for agent-driven cell runs (mirrors the
	// user-triggered execute path so badges, refresh-safe sync, and the Cancel
	// endpoint all work for agent runs).
	s.agentEngine.SetRunningFunc = s.hub.SetRunning
	s.agentEngine.UnsetRunningFunc = s.hub.UnsetRunning
	s.agentEngine.SetCancelFunc = s.hub.SetCancelFunc
	s.agentEngine.DeleteCancelFunc = s.hub.DeleteCancelFunc
	s.subdomainMW = SubdomainMiddleware(s.db.Pool)
	// Warehouse access-state worker. Enqueues arrive from membership mutations;
	// a periodic catch-up loop is added by the server bootstrap.
	s.warehouseSync = chaccess.NewSyncService(chaccess.SyncConfig{
		Reconcile: s.reconcileWarehouse,
		Logger:    slog.Default(),
	})
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.subdomainMW(s.mux).ServeHTTP(w, r)
}

// SetStorage sets the object storage backend.
func (s *Server) SetStorage(st storage.Storage) {
	s.store = st
}

// SetPlatformAdminEmail configures which email gets platform admin on registration.
func (s *Server) SetPlatformAdminEmail(email string) {
	s.platformAdminEmail = email
}

// SetDisableRegistration disables email/password registration (SSO-only mode).
func (s *Server) SetDisableRegistration(disabled bool) {
	s.disableRegistration = disabled
}

// SetPublicURL sets the base URL used when building OAuth callback URLs and
// agent resource links.
func (s *Server) SetPublicURL(u string) {
	s.publicURL = u
	s.agentEngine.SetPublicURL(u)
}

// SetFrontendURL sets the base URL used for post-auth redirects.
func (s *Server) SetFrontendURL(u string) {
	s.frontendURL = u
	s.agentEngine.SetFrontendURL(u)
}

// SetAgentStore configures the storage backend for agent session attachments.
func (s *Server) SetAgentStore(st storage.Storage) {
	s.agentEngine.SetStore(st)
}

// SetMaxAttachmentBytes sets the maximum allowed attachment upload size in bytes.
func (s *Server) SetMaxAttachmentBytes(n int64) {
	s.maxAttachmentBytes = n
}

// SetOutputLimitsMaxBytes sets the platform ceiling applied to every org's
// configured output byte caps. It is also forwarded to the agent engine so
// agent-driven cell runs enforce the same bound.
func (s *Server) SetOutputLimitsMaxBytes(n int64) {
	s.outputLimitsMaxBytes = n
	s.agentEngine.SetOutputLimitsMaxBytes(n)
}

// orgCellOutputMaxBytes returns the effective per-cell output byte cap for the
// org (0 = unlimited), clamped by the platform ceiling.
func (s *Server) orgCellOutputMaxBytes(ctx context.Context, orgID string) (int64, error) {
	var orgValue int64
	if err := s.db.Pool.QueryRow(ctx, `SELECT cell_output_max_bytes FROM orgs WHERE id = $1`, orgID).Scan(&orgValue); err != nil {
		return 0, err
	}
	return config.ResolveOutputLimit(orgValue, s.outputLimitsMaxBytes), nil
}

// orgInlineOutputsMaxBytes returns the effective notebook-inline output byte
// budget for the org (0 = unlimited), clamped by the platform ceiling.
func (s *Server) orgInlineOutputsMaxBytes(ctx context.Context, orgID string) (int64, error) {
	var orgValue int64
	if err := s.db.Pool.QueryRow(ctx, `SELECT notebook_inline_outputs_max_bytes FROM orgs WHERE id = $1`, orgID).Scan(&orgValue); err != nil {
		return 0, err
	}
	return config.ResolveOutputLimit(orgValue, s.outputLimitsMaxBytes), nil
}

// SetToolAllowedDomains sets which domains bypass the private IP block for webhook tools.
func (s *Server) SetToolAllowedDomains(domains []string) {
	s.toolAllowedDomains = domains
	s.agentEngine.SetToolAllowedDomains(domains)
}

// SetWarehouseReconcileInterval sets the periodic warehouse reconciliation
// cadence. Values <= 0 select config.DefaultWarehouseReconcileInterval. Call it
// before StartBackgroundJobs.
func (s *Server) SetWarehouseReconcileInterval(d time.Duration) {
	if d <= 0 {
		d = config.DefaultWarehouseReconcileInterval
	}
	s.warehouseReconcileInterval = d
}

// SetCHTablePermissions sets the AETHER_CH_TABLE_PERMISSIONS kill switch. It is
// a process-start setting: call it exactly once during server construction,
// before StartBackgroundJobs, and never while requests or background jobs are
// running. When disabled, every connector executes through the legacy
// stored-credential path, warehouse reconciliation is dormant, and warehouse
// deletion is DB-only; warehouse CRUD and grant APIs stay usable so admins can
// stage configuration before enabling it.
func (s *Server) SetCHTablePermissions(enabled bool) {
	s.chTablePermissions = enabled
}

// warehouseManagementEnabled is the single gate for per-user warehouse
// execution, background reconciliation, drift detection, and delete-time
// identity cleanup. Every warehouse-management path must consult it so a
// rollback cannot leave one path active behind another.
func (s *Server) warehouseManagementEnabled() bool {
	return s.chTablePermissions
}

// Close stops the warehouse reconciliation loop and then closes the sync
// worker. The worker's context is cancelled, so queued runs are dropped and
// retries stop; an in-flight reconcile may abort between statements or mid-DDL,
// leaving a partially applied plan that the next start's catch-up converges.
// Close waits for the loop goroutine to exit and for the worker's in-flight
// run to return. It is safe to call multiple times, and must run before the
// database and cache are closed because the reconcile path uses both.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		// Stop new enqueues before draining the worker so the loop cannot
		// feed work into a closing service.
		s.warehouseLoop.stop()
		if closer, ok := s.warehouseSync.(interface{ Close() }); ok {
			closer.Close()
		}
		// Stop the idle-eviction ticker before closing the pool so no
		// CloseIdle can race CloseAll; connections still leased by an
		// in-flight execution are closed by their last release.
		s.connPoolLoop.stop()
		s.connPool.CloseAll()
	})
}

// SetToolTimeoutDefault sets the fallback execution budget for agent tools
// that declare no timeout of their own.
func (s *Server) SetToolTimeoutDefault(d time.Duration) {
	s.agentEngine.SetToolTimeoutDefault(d)
}

// SetOIDCHostRewrite configures host rewriting for OIDC discovery requests.
// Used in Docker dev setups where the API container reaches Keycloak via
// a different hostname than what's in the discovery URL.
// Format: from=to, e.g. "localhost:5557=host.docker.internal:5557".
func (s *Server) SetOIDCHostRewrite(rule string) {
	if parts := strings.SplitN(rule, "=", 2); len(parts) == 2 {
		s.oidcRewriteFrom = parts[0]
		s.oidcRewriteTo = parts[1]
	}
}

// SetVersion sets build version information for the /api/v1/version endpoint.
func (s *Server) SetVersion(ver, cmt, date string) {
	s.version = ver
	s.commit = cmt
	s.buildDate = date
}

// SetFrontendHandler sets the embedded web frontend handler.
// Used as a catch-all route to serve the SPA at GET /.
func (s *Server) SetFrontendHandler(h http.Handler) {
	s.frontendHandler = h
}

// DB returns the database connection (used in tests).
func (s *Server) DB() *database.DB {
	return s.db
}

// MasterKey returns the master encryption key (used in tests).
func (s *Server) MasterKey() []byte {
	return s.masterKey
}

func (s *Server) routes() {
	authMW := AuthMiddleware(s.jwt, s.db.Pool, s.masterKey)

	// Public routes
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
	s.mux.HandleFunc("GET /swagger.json", s.handleSwaggerJSON)
	s.mux.HandleFunc("GET /docs", s.handleSwaggerUI)
	s.mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		v := s.version
		if v == "" {
			v = "dev"
		}
		c := s.commit
		if c == "" {
			c = "none"
		}
		bd := s.buildDate
		if bd == "" {
			bd = "unknown"
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"version":   v,
			"commit":    c,
			"buildDate": bd,
		})
	})
	s.mux.Handle("GET /api/v1/_diagnose/master-key", authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		test := []byte("diagnostic-ping")
		enc, err := crypto.Encrypt(test, s.masterKey)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "encrypt failed: " + err.Error()})
			return
		}
		dec, err := crypto.Decrypt(enc, s.masterKey)
		if err != nil || string(dec) != string(test) {
			writeJSON(w, 500, map[string]string{"error": "decrypt failed or mismatch: " + err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok", "key_hint": "master key is working correctly"})
	})))
	loginLimit := 10
	if v := os.Getenv("AETHER_RATE_LIMIT_LOGIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			loginLimit = n
		}
	}
	registerLimit := 5
	if v := os.Getenv("AETHER_RATE_LIMIT_REGISTER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			registerLimit = n
		}
	}
	s.mux.Handle("POST /api/v1/auth/login", s.rateLimit(rateLimitConfig{
		keyFunc: clientIP,
		limit:   loginLimit,
		window:  time.Minute,
	})(http.HandlerFunc(s.handleLogin)))
	s.mux.Handle("POST /api/v1/auth/register", s.rateLimit(rateLimitConfig{
		keyFunc: clientIP,
		limit:   registerLimit,
		window:  time.Minute,
	})(http.HandlerFunc(s.handleRegister)))
	s.mux.HandleFunc("GET /api/v1/auth/oidc/{provider}", s.handleOIDCLogin)
	s.mux.HandleFunc("GET /api/v1/auth/oidc/{provider}/callback", s.handleOIDCCallback)
	s.mux.Handle("GET /api/v1/auth/sso-providers", s.rateLimit(rateLimitConfig{
		keyFunc: clientIP,
		limit:   20,
		window:  time.Minute,
	})(http.HandlerFunc(s.handleSSOProbe)))
	s.mux.HandleFunc("GET /api/v1/auth/config", s.handleRegistrationStatus)

	// Onboarding routes (require auth but allow onboarding role)
	s.mux.Handle("POST /api/v1/auth/org/create", authMW(http.HandlerFunc(s.handleOrgCreate)))
	s.mux.Handle("POST /api/v1/auth/org/join", authMW(http.HandlerFunc(s.handleOrgJoin)))
	// Invite routes (org admin)
	s.mux.Handle("POST /api/v1/members/invite-link", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateInviteLink))))

	// User routes
	s.mux.Handle("GET /api/v1/users/me", authMW(http.HandlerFunc(s.handleGetCurrentUser)))
	s.mux.Handle("PUT /api/v1/users/me", authMW(http.HandlerFunc(s.handleUpdateCurrentUser)))

	// Personal access token routes
	s.mux.Handle("POST /api/v1/tokens", authMW(http.HandlerFunc(s.handleCreateToken)))
	s.mux.Handle("GET /api/v1/tokens", authMW(http.HandlerFunc(s.handleListTokens)))
	s.mux.Handle("DELETE /api/v1/tokens/{id}", authMW(http.HandlerFunc(s.handleDeleteToken)))

	// Notebook routes
	s.mux.Handle("POST /api/v1/notebooks", authMW(http.HandlerFunc(s.handleCreateNotebook)))
	s.mux.Handle("GET /api/v1/notebooks", authMW(http.HandlerFunc(s.handleListNotebooks)))
	s.mux.Handle("GET /api/v1/notebooks/{id}", authMW(http.HandlerFunc(s.handleGetNotebook)))
	s.mux.Handle("DELETE /api/v1/notebooks/{id}", authMW(s.requirePermission("notebook", "id", "delete")(http.HandlerFunc(s.handleDeleteNotebook))))
	s.mux.Handle("PUT /api/v1/notebooks/{id}", authMW(s.requirePermission("notebook", "id", "edit")(http.HandlerFunc(s.handleUpdateNotebook))))
	s.mux.Handle("GET /api/v1/notebooks/{id}/permissions", authMW(http.HandlerFunc(s.handleGetNotebookPermissions)))
	s.mux.Handle("GET /api/v1/notebooks/{id}/export", authMW(http.HandlerFunc(s.handleExportNotebook)))
	s.mux.Handle("POST /api/v1/notebooks/import", authMW(http.HandlerFunc(s.handleImportNotebook)))
	s.mux.Handle("POST /api/v1/notebooks/{id}/clone", authMW(http.HandlerFunc(s.handleCloneNotebook)))
	s.mux.Handle("GET /api/v1/notebooks/{id}/share", authMW(s.requirePermission("notebook", "id", "view")(http.HandlerFunc(s.handleGetNotebookShare))))
	s.mux.Handle("POST /api/v1/notebooks/{id}/share", authMW(s.requirePermission("notebook", "id", "share")(http.HandlerFunc(s.handleShareNotebook))))
	s.mux.Handle("DELETE /api/v1/notebooks/{id}/share", authMW(s.requirePermission("notebook", "id", "share")(http.HandlerFunc(s.handleRevokeNotebookShare))))

	// Cell routes
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells", authMW(http.HandlerFunc(s.handleCreateCell)))
	s.mux.Handle("PUT /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(http.HandlerFunc(s.handleUpdateCell)))
	s.mux.Handle("DELETE /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(http.HandlerFunc(s.handleDeleteCell)))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/execute", authMW(http.HandlerFunc(s.handleExecuteCell)))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/cancel", authMW(http.HandlerFunc(s.handleCancelCell)))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/duplicate", authMW(http.HandlerFunc(s.handleDuplicateCell)))

	// Cell history routes
	s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/cells/{cell_id}/versions", authMW(http.HandlerFunc(s.handleListCellVersions)))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/versions/{version_id}/restore", authMW(http.HandlerFunc(s.handleRestoreCellVersion)))

	// Snapshot routes
	s.mux.Handle("POST /api/v1/notebooks/{id}/snapshots", authMW(http.HandlerFunc(s.handleCreateSnapshot)))
	s.mux.Handle("GET /api/v1/notebooks/{id}/snapshots", authMW(http.HandlerFunc(s.handleListSnapshots)))
	s.mux.Handle("POST /api/v1/notebooks/{id}/snapshots/{snapshot_id}/restore", authMW(http.HandlerFunc(s.handleRestoreSnapshot)))
	s.mux.Handle("GET /api/v1/notebooks/{id}/snapshots/{snapshot_id}/diff", authMW(http.HandlerFunc(s.handleSnapshotDiff)))

	// Schedule routes
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/schedules", authMW(http.HandlerFunc(s.handleCreateSchedule)))
	s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/schedules", authMW(http.HandlerFunc(s.handleListSchedules)))
	s.mux.Handle("GET /api/v1/schedules/{id}", authMW(http.HandlerFunc(s.handleGetSchedule)))
	s.mux.Handle("DELETE /api/v1/schedules/{id}", authMW(http.HandlerFunc(s.handleDeleteSchedule)))
	s.mux.Handle("PUT /api/v1/schedules/{id}", authMW(http.HandlerFunc(s.handleUpdateSchedule)))

	// Dashboard routes
	s.mux.Handle("POST /api/v1/dashboards", authMW(http.HandlerFunc(s.handleCreateDashboard)))
	s.mux.Handle("GET /api/v1/dashboards", authMW(http.HandlerFunc(s.handleListDashboards)))
	s.mux.Handle("GET /api/v1/dashboards/{id}", authMW(http.HandlerFunc(s.handleGetDashboard)))
	s.mux.Handle("PUT /api/v1/dashboards/{id}", authMW(s.requirePermission("dashboard", "id", "edit")(http.HandlerFunc(s.handleUpdateDashboard))))
	s.mux.Handle("DELETE /api/v1/dashboards/{id}", authMW(s.requirePermission("dashboard", "id", "delete")(http.HandlerFunc(s.handleDeleteDashboard))))
	s.mux.Handle("POST /api/v1/dashboards/{id}/widgets", authMW(http.HandlerFunc(s.handleAddWidget)))
	s.mux.Handle("PUT /api/v1/dashboards/{id}/widgets/{widget_id}", authMW(http.HandlerFunc(s.handleUpdateWidget)))
	s.mux.Handle("DELETE /api/v1/dashboards/{id}/widgets/{widget_id}", authMW(http.HandlerFunc(s.handleDeleteWidget)))
	s.mux.Handle("GET /api/v1/dashboards/{id}/share", authMW(s.requirePermission("dashboard", "id", "view")(http.HandlerFunc(s.handleGetDashboardShare))))
	s.mux.Handle("POST /api/v1/dashboards/{id}/share", authMW(s.requirePermission("dashboard", "id", "share")(http.HandlerFunc(s.handleShareDashboard))))
	s.mux.Handle("DELETE /api/v1/dashboards/{id}/share", authMW(s.requirePermission("dashboard", "id", "share")(http.HandlerFunc(s.handleRevokeDashboardShare))))
	s.mux.Handle("GET /api/v1/dashboards/{id}/permissions", authMW(http.HandlerFunc(s.handleGetDashboardPermissions)))
	s.mux.HandleFunc("GET /api/v1/public/{token}", s.handlePublicResource)
	s.mux.HandleFunc("GET /api/v1/public/motd", s.handleListLoginMOTD)

	// Public sharing settings
	s.mux.Handle("GET /api/v1/org/sharing", authMW(http.HandlerFunc(s.handleGetOrgSharingSettings)))
	s.mux.Handle("PUT /api/v1/org/sharing", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateOrgSharingSettings))))

	// Invitation settings
	s.mux.Handle("GET /api/v1/org/invitations", authMW(http.HandlerFunc(s.handleGetOrgInvitationSettings)))
	s.mux.Handle("PUT /api/v1/org/invitations", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateOrgInvitationSettings))))

	// Registration settings
	s.mux.Handle("GET /api/v1/org/registration", authMW(http.HandlerFunc(s.handleGetOrgRegistrationSettings)))
	s.mux.Handle("PUT /api/v1/org/registration", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateOrgRegistrationSettings))))

	// Data export settings
	s.mux.Handle("GET /api/v1/org/data-export", authMW(http.HandlerFunc(s.handleGetOrgDataExportSettings)))
	s.mux.Handle("PUT /api/v1/org/data-export", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateOrgDataExportSettings))))

	// Org settings (JSONB settings column)
	s.mux.Handle("GET /api/v1/org/settings", authMW(RequireRole("admin")(http.HandlerFunc(s.handleGetOrgSettings))))
	s.mux.Handle("PUT /api/v1/org/settings", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateOrgSettings))))

	// Org output limits
	s.mux.Handle("GET /api/v1/org/output-limits", authMW(RequireRole("admin")(http.HandlerFunc(s.handleGetOrgOutputLimits))))
	s.mux.Handle("PUT /api/v1/org/output-limits", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateOrgOutputLimits))))

	// Cell output download
	s.mux.Handle("GET /api/v1/cells/{id}/outputs/download", authMW(http.HandlerFunc(s.handleCellOutputsDownload)))

	// WebSocket routes
	s.mux.Handle("GET /api/v1/ws/notebooks/{id}", authMW(http.HandlerFunc(s.handleNotebookWS)))

	// Internal routes (called by Hocuspocus relay only)
	s.mux.HandleFunc("GET /internal/yjs/{notebook_id}", s.handleInternalYjsGet)
	s.mux.HandleFunc("PUT /internal/yjs/{notebook_id}", s.handleInternalYjsPut)
	s.mux.HandleFunc("GET /internal/auth/validate", s.handleInternalAuthValidate)

	// Attachment routes
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/attachments", authMW(http.HandlerFunc(s.handleUploadAttachment)))
	s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/attachments", authMW(http.HandlerFunc(s.handleListAttachments)))
	s.mux.Handle("GET /api/v1/attachments/{id}", authMW(http.HandlerFunc(s.handleGetAttachment)))
	s.mux.Handle("DELETE /api/v1/attachments/{id}", authMW(http.HandlerFunc(s.handleDeleteAttachment)))

	// Connector routes
	s.mux.Handle("POST /api/v1/connectors", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateConnector))))
	s.mux.Handle("POST /api/v1/connectors/test", authMW(RequireRole("admin")(http.HandlerFunc(s.handleTestConnectorConfig))))
	s.mux.Handle("GET /api/v1/connectors", authMW(http.HandlerFunc(s.handleListConnectors)))
	s.mux.Handle("GET /api/v1/connectors/{id}", authMW(s.requirePermission("connector", "id", "view")(http.HandlerFunc(s.handleGetConnector))))
	s.mux.Handle("PUT /api/v1/connectors/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateConnector))))
	s.mux.Handle("DELETE /api/v1/connectors/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteConnector))))
	s.mux.Handle("PUT /api/v1/connectors/{id}/default", authMW(RequireRole("admin")(http.HandlerFunc(s.handleSetDefaultConnector))))
	s.mux.Handle("POST /api/v1/connectors/{id}/test", authMW(http.HandlerFunc(s.handleTestConnector)))
	s.mux.Handle("GET /api/v1/connectors/{id}/schema", authMW(http.HandlerFunc(s.handleConnectorSchema)))
	s.mux.Handle("GET /api/v1/connectors/{id}/databases", authMW(http.HandlerFunc(s.handleListConnectorDatabases)))
	s.mux.Handle("PUT /api/v1/connectors/{id}/warehouse", authMW(RequireRole("admin")(http.HandlerFunc(s.handleSetConnectorWarehouse))))

	// Warehouse routes (org admin)
	s.mux.Handle("GET /api/v1/warehouses", authMW(RequireRole("admin")(http.HandlerFunc(s.handleListWarehouses))))
	s.mux.Handle("POST /api/v1/warehouses", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateWarehouse))))
	s.mux.Handle("GET /api/v1/warehouses/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleGetWarehouse))))
	s.mux.Handle("PUT /api/v1/warehouses/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateWarehouse))))
	s.mux.Handle("DELETE /api/v1/warehouses/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteWarehouse))))
	s.mux.Handle("PUT /api/v1/warehouses/{id}/provisioner", authMW(RequireRole("admin")(http.HandlerFunc(s.handleSetWarehouseProvisioner))))

	// Warehouse table grants and service routing (handlers scope by org:
	// mutations and listing are org admin, effective-access is org admin or
	// self, preference is self).
	s.mux.Handle("GET /api/v1/warehouses/{id}/grants", authMW(RequireRole("admin")(http.HandlerFunc(s.handleListWarehouseGrants))))
	s.mux.Handle("POST /api/v1/warehouses/{id}/grants", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateWarehouseGrant))))
	s.mux.Handle("DELETE /api/v1/warehouses/{id}/grants/{grant_id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteWarehouseGrant))))
	s.mux.Handle("GET /api/v1/warehouses/{id}/effective-access", authMW(http.HandlerFunc(s.handleWarehouseEffectiveAccess)))
	s.mux.Handle("PUT /api/v1/warehouses/{id}/preference", authMW(http.HandlerFunc(s.handleSetWarehousePreference)))
	s.mux.Handle("GET /api/v1/warehouses/{id}/new-tables", authMW(RequireRole("admin")(http.HandlerFunc(s.handleWarehouseNewTables))))
	s.mux.Handle("GET /api/v1/warehouses/{id}/validation", authMW(RequireRole("admin")(http.HandlerFunc(s.handleWarehouseValidation))))

	// Recent route
	s.mux.Handle("GET /api/v1/recent", authMW(http.HandlerFunc(s.handleGetRecent)))

	// Trash routes
	s.mux.Handle("GET /api/v1/trash", authMW(http.HandlerFunc(s.handleListTrash)))
	s.mux.Handle("POST /api/v1/trash/restore", authMW(http.HandlerFunc(s.handleRestoreFromTrash)))

	// Home route - lists all home folders for the current org
	s.mux.Handle("GET /api/v1/home", authMW(http.HandlerFunc(s.handleListHomeFolders)))
	// Ensure home folder exists for current user (creates if missing)
	s.mux.Handle("POST /api/v1/users/me/home", authMW(http.HandlerFunc(s.handleEnsureHomeFolder)))

	// Folder routes
	s.mux.Handle("GET /api/v1/folders", authMW(http.HandlerFunc(s.handleListRootContents)))
	s.mux.Handle("GET /api/v1/folders/{id}", authMW(http.HandlerFunc(s.handleGetFolderContents)))
	s.mux.Handle("GET /api/v1/folders/{id}/ancestors", authMW(http.HandlerFunc(s.handleGetFolderAncestors)))
	s.mux.Handle("POST /api/v1/folders", authMW(http.HandlerFunc(s.handleCreateFolder)))
	s.mux.Handle("PUT /api/v1/folders/{id}", authMW(s.requirePermission("folder", "id", "edit")(http.HandlerFunc(s.handleUpdateFolder))))
	s.mux.Handle("DELETE /api/v1/folders/{id}", authMW(s.requirePermission("folder", "id", "delete")(http.HandlerFunc(s.handleDeleteFolder))))

	// Group routes
	s.mux.Handle("GET /api/v1/groups", authMW(http.HandlerFunc(s.handleListGroups)))
	s.mux.Handle("POST /api/v1/groups", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateGroup))))
	s.mux.Handle("PUT /api/v1/groups/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateGroup))))
	s.mux.Handle("DELETE /api/v1/groups/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteGroup))))
	s.mux.Handle("GET /api/v1/groups/{id}/members", authMW(http.HandlerFunc(s.handleListGroupMembers)))
	s.mux.Handle("POST /api/v1/groups/{id}/members", authMW(RequireRole("admin")(http.HandlerFunc(s.handleAddGroupMember))))
	s.mux.Handle("DELETE /api/v1/groups/{id}/members/{user_id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleRemoveGroupMember))))
	s.mux.Handle("GET /api/v1/groups/{id}/pending-members", authMW(http.HandlerFunc(s.handleListPendingGroupMembers)))
	s.mux.Handle("POST /api/v1/groups/{id}/pending-members", authMW(RequireRole("admin")(http.HandlerFunc(s.handleAddPendingGroupMembers))))
	s.mux.Handle("DELETE /api/v1/groups/{id}/pending-members/{email}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleRemovePendingGroupMember))))

	// ACL routes
	s.mux.Handle("GET /api/v1/acl/{resource_type}/{resource_id}", authMW(http.HandlerFunc(s.handleGetACL)))
	s.mux.Handle("PUT /api/v1/acl/{resource_type}/{resource_id}", authMW(http.HandlerFunc(s.handlePutACL)))

	// Template routes
	s.mux.Handle("POST /api/v1/templates", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateTemplate))))
	s.mux.Handle("GET /api/v1/templates", authMW(http.HandlerFunc(s.handleListTemplates)))
	s.mux.Handle("DELETE /api/v1/templates/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteTemplate))))

	// Platform admin routes
	s.mux.Handle("GET /api/v1/admin/orgs", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListOrgs))))
	s.mux.Handle("POST /api/v1/admin/orgs", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminCreateOrg))))
	s.mux.Handle("DELETE /api/v1/admin/orgs/{id}", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminDeleteOrg))))
	s.mux.Handle("GET /api/v1/admin/users", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListUsers))))
	s.mux.Handle("PUT /api/v1/admin/users/{id}", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminUpdateUser))))
	s.mux.Handle("DELETE /api/v1/admin/users/{id}", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminDeleteUser))))
	s.mux.Handle("GET /api/v1/admin/sso/providers", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListSSOProviders))))
	s.mux.Handle("POST /api/v1/admin/sso/providers", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminCreateSSOProvider))))
	s.mux.Handle("PUT /api/v1/admin/sso/providers/{id}", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminUpdateSSOProvider))))
	s.mux.Handle("DELETE /api/v1/admin/sso/providers/{id}", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminDeleteSSOProvider))))
	s.mux.Handle("POST /api/v1/admin/sso/providers/{id}/test", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminTestSSOProvider))))
	s.mux.Handle("GET /api/v1/admin/audit/s3-config", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handlePlatformGetAuditS3Config))))
	s.mux.Handle("PUT /api/v1/admin/audit/s3-config", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handlePlatformUpdateAuditS3Config))))
	s.mux.Handle("POST /api/v1/admin/audit/s3-config/test", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handlePlatformTestAuditS3Config))))

	// Org-level audit S3 config routes
	s.mux.Handle("GET /api/v1/audit/s3-config", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgGetAuditS3Config))))
	s.mux.Handle("PUT /api/v1/audit/s3-config", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgUpdateAuditS3Config))))
	s.mux.Handle("POST /api/v1/audit/s3-config/test", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgTestAuditS3Config))))

	// Org admin SSO routes
	s.mux.Handle("GET /api/v1/sso/providers", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgListSSOProviders))))
	s.mux.Handle("POST /api/v1/sso/providers", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgCreateSSOProvider))))
	s.mux.Handle("PUT /api/v1/sso/providers/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgUpdateSSOProvider))))
	s.mux.Handle("DELETE /api/v1/sso/providers/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgDeleteSSOProvider))))
	s.mux.Handle("GET /api/v1/sso/platform-providers", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgListPlatformProviders))))
	s.mux.Handle("POST /api/v1/sso/platform-providers/{id}/enable", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgEnablePlatformProvider))))
	s.mux.Handle("DELETE /api/v1/sso/platform-providers/{id}/enable", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgDisablePlatformProvider))))
	s.mux.Handle("GET /api/v1/sso/settings", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgGetSSOSettings))))
	s.mux.Handle("PUT /api/v1/sso/settings", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgUpdateSSOSettings))))
	s.mux.Handle("POST /api/v1/sso/providers/test", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgTestSSOProvider))))

	// Audit routes
	s.mux.Handle("GET /api/v1/audit", authMW(RequireRole("admin")(http.HandlerFunc(s.handleListAuditLogs))))

	// Member routes
	s.mux.Handle("GET /api/v1/members", authMW(http.HandlerFunc(s.handleListMembers)))
	s.mux.Handle("POST /api/v1/members", authMW(RequireRole("admin")(http.HandlerFunc(s.handleInviteMember))))
	s.mux.Handle("POST /api/v1/members/invite", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateInvite))))
	s.mux.Handle("PUT /api/v1/members/{user_id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateMemberRole))))
	s.mux.Handle("DELETE /api/v1/members/{user_id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleRemoveMember))))

	// Agent routes
	ah := agentHandlers{server: s}
	s.mux.Handle("GET /api/v1/agents", authMW(http.HandlerFunc(ah.handleListAgents)))
	s.mux.Handle("POST /api/v1/agents", authMW(http.HandlerFunc(ah.handleCreateAgent)))
	s.mux.Handle("GET /api/v1/agents/{id}", authMW(http.HandlerFunc(ah.handleGetAgent)))
	s.mux.Handle("PUT /api/v1/agents/{id}", authMW(s.requirePermission("agent", "id", "edit")(http.HandlerFunc(ah.handleUpdateAgent))))
	s.mux.Handle("DELETE /api/v1/agents/{id}", authMW(s.requirePermission("agent", "id", "delete")(http.HandlerFunc(ah.handleDeleteAgent))))
	s.mux.Handle("POST /api/v1/agents/{id}/session", authMW(http.HandlerFunc(ah.handleCreateSession)))
	s.mux.Handle("GET /api/v1/agents/{id}/sessions", authMW(http.HandlerFunc(ah.handleListSessions)))
	s.mux.Handle("GET /api/v1/sessions/{session_id}", authMW(http.HandlerFunc(ah.handleGetSession)))
	s.mux.Handle("GET /api/v1/sessions/{session_id}/messages", authMW(http.HandlerFunc(ah.handleGetSessionMessages)))
	s.mux.Handle("GET /api/v1/agents/sessions/{id}/usage", authMW(http.HandlerFunc(ah.handleGetSessionUsage)))
	s.mux.Handle("GET /api/v1/agents/subagent/{task_id}/messages", authMW(http.HandlerFunc(ah.handleGetSubagentMessages)))
	s.mux.Handle("PATCH /api/v1/sessions/{session_id}/title", authMW(http.HandlerFunc(ah.handleUpdateSessionTitle)))
	mch := modelConfigHandlers{server: s}
	s.mux.Handle("GET /api/v1/model-configs", authMW(http.HandlerFunc(mch.handleList)))
	s.mux.Handle("GET /api/v1/model-configs/{id}", authMW(s.requirePermission("model_config", "id", "view")(http.HandlerFunc(mch.handleGet))))
	s.mux.Handle("POST /api/v1/model-configs", authMW(http.HandlerFunc(mch.handleCreate)))
	s.mux.Handle("PUT /api/v1/model-configs/{id}", authMW(s.requirePermission("model_config", "id", "edit")(http.HandlerFunc(mch.handleUpdate))))
	s.mux.Handle("DELETE /api/v1/model-configs/{id}", authMW(s.requirePermission("model_config", "id", "delete")(http.HandlerFunc(mch.handleDelete))))
	s.mux.Handle("POST /api/v1/model-configs/{id}/test", authMW(http.HandlerFunc(mch.handleTest)))
	sh := skillHandlers{server: s}
	s.mux.Handle("GET /api/v1/skills", authMW(http.HandlerFunc(sh.handleList)))
	s.mux.Handle("GET /api/v1/skills/{id}", authMW(s.requirePermission("skill", "id", "view")(http.HandlerFunc(sh.handleGet))))
	s.mux.Handle("POST /api/v1/skills", authMW(http.HandlerFunc(sh.handleCreate)))
	s.mux.Handle("PUT /api/v1/skills/{id}", authMW(s.requirePermission("skill", "id", "edit")(http.HandlerFunc(sh.handleUpdate))))
	s.mux.Handle("DELETE /api/v1/skills/{id}", authMW(s.requirePermission("skill", "id", "delete")(http.HandlerFunc(sh.handleDelete))))
	th := toolHandlers{server: s}
	s.mux.Handle("GET /api/v1/tools", authMW(http.HandlerFunc(th.handleList)))
	s.mux.Handle("POST /api/v1/tools", authMW(RequireRole("admin")(http.HandlerFunc(th.handleCreate))))
	s.mux.Handle("GET /api/v1/tools/{id}", authMW(s.requirePermission("tool", "id", "view")(http.HandlerFunc(th.handleGet))))
	s.mux.Handle("PUT /api/v1/tools/{id}", authMW(s.requirePermission("tool", "id", "edit")(http.HandlerFunc(th.handleUpdate))))
	s.mux.Handle("DELETE /api/v1/tools/{id}", authMW(s.requirePermission("tool", "id", "delete")(http.HandlerFunc(th.handleDelete))))
	s.mux.Handle("POST /api/v1/tools/{id}/test", authMW(http.HandlerFunc(th.handleTest)))
	mh := mcpServerHandlers{server: s}
	s.mux.Handle("GET /api/v1/mcp-servers", authMW(http.HandlerFunc(mh.handleList)))
	s.mux.Handle("POST /api/v1/mcp-servers", authMW(RequireRole("admin")(http.HandlerFunc(mh.handleCreate))))
	s.mux.Handle("GET /api/v1/mcp-servers/{id}", authMW(http.HandlerFunc(mh.handleGet)))
	s.mux.Handle("PUT /api/v1/mcp-servers/{id}", authMW(RequireRole("admin")(http.HandlerFunc(mh.handleUpdate))))
	s.mux.Handle("DELETE /api/v1/mcp-servers/{id}", authMW(RequireRole("admin")(http.HandlerFunc(mh.handleDelete))))
	s.mux.Handle("POST /api/v1/mcp-servers/{id}/test", authMW(RequireRole("admin")(http.HandlerFunc(mh.handleTestMCPServer))))

	// MCP protocol endpoint (exposes built-in tools via Model Context Protocol)
	s.mux.Handle("POST /api/v1/mcp", authMW(http.HandlerFunc(s.handleMCP)))
	// Streamable HTTP clients probe GET for an SSE stream; Aether has none.
	// Register explicit 405s so the SPA catch-all never answers these.
	s.mux.Handle("GET /api/v1/mcp", http.HandlerFunc(handleMCPNoStream))
	s.mux.Handle("DELETE /api/v1/mcp", http.HandlerFunc(handleMCPNoStream))

	// Agent session attachment routes (vision support)
	s.mux.Handle("POST /api/v1/agent-sessions/{session_id}/attachments", authMW(http.HandlerFunc(s.handleUploadAgentAttachment)))
	s.mux.Handle("GET /api/v1/agent-attachments/{id}", authMW(http.HandlerFunc(s.handleGetAgentAttachment)))

	// Agent WebSocket route
	s.mux.Handle("GET /api/v1/ws/agents/{session_id}", authMW(http.HandlerFunc(s.handleAgentWS)))

	// Agent stats routes
	s.mux.Handle("GET /api/v1/agents/stats", authMW(RequireRole("admin")(http.HandlerFunc(ah.handleAgentStats))))
	s.mux.Handle("POST /api/v1/agents/stats/rollup", authMW(RequireRole("admin")(http.HandlerFunc(ah.handleAgentStatsRollup))))
	s.mux.Handle("GET /api/v1/agents/{id}/stats", authMW(RequireRole("admin")(http.HandlerFunc(ah.handleAgentStatsByAgent))))

	// MOTD routes
	s.mux.Handle("GET /api/v1/motd", authMW(http.HandlerFunc(s.handleListMOTD)))
	s.mux.Handle("GET /api/v1/admin/motd", authMW(RequireRole("admin")(http.HandlerFunc(s.handleListMOTDAdmin))))
	s.mux.Handle("POST /api/v1/admin/motd", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateMOTD))))
	s.mux.Handle("PUT /api/v1/admin/motd/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateMOTD))))
	s.mux.Handle("DELETE /api/v1/admin/motd/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteMOTD))))

	// Serve embedded frontend SPA — must be last, API routes take priority.
	// Uses dynamic dispatch because frontendHandler is set after NewServer returns.
	s.mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.frontendHandler != nil {
			s.frontendHandler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	}))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := map[string]string{}

	if err := s.db.Pool.Ping(ctx); err != nil {
		checks["database"] = "unreachable"
	} else {
		checks["database"] = "ok"
	}

	if s.Cache != nil {
		if err := s.Cache.Ping(ctx); err != nil {
			checks["redis"] = "unreachable"
		} else {
			checks["redis"] = "ok"
		}
	}

	status := http.StatusOK
	for _, v := range checks {
		if v != "ok" {
			status = http.StatusServiceUnavailable
			break
		}
	}

	writeJSON(w, status, map[string]any{
		"status": status == http.StatusOK,
		"checks": checks,
	})
}
