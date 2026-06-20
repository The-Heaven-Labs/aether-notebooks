package api

import (
	"context"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/heavenlabs/hnb/internal/agent"
	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/cache"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/database"
	"github.com/heavenlabs/hnb/internal/storage"
)

type Server struct {
	db                 *database.DB
	jwt                *auth.JWTIssuer
	audit              *audit.Logger
	masterKey          []byte
	hub                *Hub
	mux                *http.ServeMux
	store              storage.Storage
	platformAdminEmail string
	publicURL          string
	frontendURL        string
	Cache              *cache.Cache
	maxAttachmentBytes int64
	agentEngine        *agent.Engine
	upgrader           websocket.Upgrader
}

func NewServer(db *database.DB, jwt *auth.JWTIssuer, auditLogger *audit.Logger, masterKey []byte, redisCache *cache.Cache) *Server {
	s := &Server{
		db:        db,
		jwt:       jwt,
		audit:     auditLogger,
		masterKey: masterKey,
		hub:       NewHub(),
		mux:       http.NewServeMux(),
		Cache:     redisCache,
		upgrader:  websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}
	s.agentEngine = agent.NewEngine(context.Background(), db.Pool)
	s.agentEngine.BroadcastFunc = func(notebookID string, msg any) {
		s.hub.Broadcast(notebookID, msg)
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// SetStorage sets the object storage backend.
func (s *Server) SetStorage(st storage.Storage) {
	s.store = st
}

// SetPlatformAdminEmail configures which email gets platform admin on registration.
func (s *Server) SetPlatformAdminEmail(email string) {
	s.platformAdminEmail = email
}

// SetPublicURL sets the base URL used when building OAuth callback URLs.
func (s *Server) SetPublicURL(u string) {
	s.publicURL = u
}

// SetFrontendURL sets the base URL used for post-auth redirects.
func (s *Server) SetFrontendURL(u string) {
	s.frontendURL = u
}

// SetMaxAttachmentBytes sets the maximum allowed attachment upload size in bytes.
func (s *Server) SetMaxAttachmentBytes(n int64) {
	s.maxAttachmentBytes = n
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
	authMW := AuthMiddleware(s.jwt, s.db.Pool)

	// Public routes
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /swagger.json", s.handleSwaggerJSON)
	s.mux.HandleFunc("GET /docs", s.handleSwaggerUI)
	s.mux.HandleFunc("GET /api/v1/_diagnose/master-key", func(w http.ResponseWriter, r *http.Request) {
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
	})
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	s.mux.HandleFunc("GET /api/v1/auth/oidc/{provider}", s.handleOIDCLogin)
	s.mux.HandleFunc("GET /api/v1/auth/oidc/{provider}/callback", s.handleOIDCCallback)
	s.mux.HandleFunc("GET /api/v1/auth/sso-providers", s.handleSSOProbe)

	// Onboarding routes (require auth but allow onboarding role)
	s.mux.Handle("POST /api/v1/auth/org/create", authMW(http.HandlerFunc(s.handleOrgCreate)))
	s.mux.Handle("POST /api/v1/auth/org/join", authMW(http.HandlerFunc(s.handleOrgJoin)))
	// Invite routes (org admin)
	s.mux.Handle("POST /api/v1/members/invite", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateInvite))))
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

	// Cell routes
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells", authMW(http.HandlerFunc(s.handleCreateCell)))
	s.mux.Handle("PUT /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(http.HandlerFunc(s.handleUpdateCell)))
	s.mux.Handle("DELETE /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(http.HandlerFunc(s.handleDeleteCell)))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/execute", authMW(http.HandlerFunc(s.handleExecuteCell)))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/duplicate", authMW(http.HandlerFunc(s.handleDuplicateCell)))

	// Cell history routes
	s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/cells/{cell_id}/versions", authMW(http.HandlerFunc(s.handleListCellVersions)))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/versions/{version_id}/restore", authMW(http.HandlerFunc(s.handleRestoreCellVersion)))

	// Snapshot routes
	s.mux.Handle("POST /api/v1/notebooks/{id}/snapshots", authMW(http.HandlerFunc(s.handleCreateSnapshot)))
	s.mux.Handle("GET /api/v1/notebooks/{id}/snapshots", authMW(http.HandlerFunc(s.handleListSnapshots)))
	s.mux.Handle("POST /api/v1/notebooks/{id}/snapshots/{snapshot_id}/restore", authMW(http.HandlerFunc(s.handleRestoreSnapshot)))

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
	s.mux.Handle("POST /api/v1/dashboards/{id}/share", authMW(http.HandlerFunc(s.handleShareDashboard)))
	s.mux.Handle("GET /api/v1/dashboards/{id}/permissions", authMW(http.HandlerFunc(s.handleGetDashboardPermissions)))
	s.mux.HandleFunc("GET /api/v1/public/dashboards/{token}", s.handlePublicDashboard)
	s.mux.HandleFunc("GET /api/v1/public/motd", s.handleListLoginMOTD)

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
	s.mux.Handle("POST /api/v1/connectors/test", authMW(http.HandlerFunc(s.handleTestConnectorConfig)))
	s.mux.Handle("GET /api/v1/connectors", authMW(http.HandlerFunc(s.handleListConnectors)))
	s.mux.Handle("GET /api/v1/connectors/{id}", authMW(s.requirePermission("connector", "id", "view")(http.HandlerFunc(s.handleGetConnector))))
	s.mux.Handle("PUT /api/v1/connectors/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateConnector))))
	s.mux.Handle("DELETE /api/v1/connectors/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteConnector))))
	s.mux.Handle("PUT /api/v1/connectors/{id}/default", authMW(RequireRole("admin")(http.HandlerFunc(s.handleSetDefaultConnector))))
	s.mux.Handle("POST /api/v1/connectors/{id}/test", authMW(http.HandlerFunc(s.handleTestConnector)))
	s.mux.Handle("GET /api/v1/connectors/{id}/schema", authMW(http.HandlerFunc(s.handleConnectorSchema)))
	s.mux.Handle("GET /api/v1/connectors/{id}/databases", authMW(http.HandlerFunc(s.handleListConnectorDatabases)))

	// Recent route
	s.mux.Handle("GET /api/v1/recent", authMW(http.HandlerFunc(s.handleGetRecent)))

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

	// ACL routes
	s.mux.Handle("GET /api/v1/acl/{resource_type}/{resource_id}", authMW(http.HandlerFunc(s.handleGetACL)))
	s.mux.Handle("PUT /api/v1/acl/{resource_type}/{resource_id}", authMW(http.HandlerFunc(s.handlePutACL)))

	// Template routes
	s.mux.Handle("POST /api/v1/templates", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateTemplate))))
	s.mux.Handle("GET /api/v1/templates", authMW(http.HandlerFunc(s.handleListTemplates)))
	s.mux.Handle("DELETE /api/v1/templates/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteTemplate))))

	// Platform admin routes
	s.mux.Handle("GET /api/v1/admin/orgs", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListOrgs))))
	s.mux.Handle("GET /api/v1/admin/users", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListUsers))))
	s.mux.Handle("PUT /api/v1/admin/users/{id}", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminUpdateUser))))
	s.mux.Handle("GET /api/v1/admin/sso/providers", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListSSOProviders))))
	s.mux.Handle("POST /api/v1/admin/sso/providers", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminCreateSSOProvider))))
	s.mux.Handle("PUT /api/v1/admin/sso/providers/{id}", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminUpdateSSOProvider))))
	s.mux.Handle("DELETE /api/v1/admin/sso/providers/{id}", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminDeleteSSOProvider))))
	s.mux.Handle("POST /api/v1/admin/sso/providers/{id}/test", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminTestSSOProvider))))

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
	s.mux.Handle("POST /api/v1/tools", authMW(http.HandlerFunc(th.handleCreate)))
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

	// Agent WebSocket route
	s.mux.Handle("GET /api/v1/ws/agents/{session_id}", authMW(http.HandlerFunc(s.handleAgentWS)))

	// Agent stats routes
	s.mux.Handle("GET /api/v1/agents/stats", authMW(RequireRole("admin")(http.HandlerFunc(ah.handleAgentStats))))
	s.mux.Handle("GET /api/v1/agents/{id}/stats", authMW(RequireRole("admin")(http.HandlerFunc(ah.handleAgentStatsByAgent))))

	// MOTD routes
	s.mux.Handle("GET /api/v1/motd", authMW(http.HandlerFunc(s.handleListMOTD)))
	s.mux.Handle("GET /api/v1/admin/motd", authMW(RequireRole("admin")(http.HandlerFunc(s.handleListMOTDAdmin))))
	s.mux.Handle("POST /api/v1/admin/motd", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateMOTD))))
	s.mux.Handle("PUT /api/v1/admin/motd/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateMOTD))))
	s.mux.Handle("DELETE /api/v1/admin/motd/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteMOTD))))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
