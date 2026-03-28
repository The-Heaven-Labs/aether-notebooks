package api

import (
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/database"
)

type Server struct {
	db            *database.DB
	jwt           *auth.JWTIssuer
	audit         *audit.Logger
	masterKey     []byte
	hub           *Hub
	mux           *http.ServeMux
	oidcProviders map[string]auth.OIDCProvider
	attachmentDir string
}

func NewServer(db *database.DB, jwt *auth.JWTIssuer, auditLogger *audit.Logger, masterKey []byte, oidcProviders map[string]auth.OIDCProvider) *Server {
	s := &Server{
		db:            db,
		jwt:           jwt,
		audit:         auditLogger,
		masterKey:     masterKey,
		hub:           NewHub(),
		mux:           http.NewServeMux(),
		oidcProviders: oidcProviders,
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// SetAttachmentDir sets the directory where uploaded attachments are stored.
func (s *Server) SetAttachmentDir(dir string) {
	s.attachmentDir = dir
}

func (s *Server) routes() {
	authMW := AuthMiddleware(s.jwt)

	// Public routes
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	s.mux.HandleFunc("GET /api/v1/auth/oidc/{provider}", s.handleOIDCLogin)
	s.mux.HandleFunc("GET /api/v1/auth/oidc/{provider}/callback", s.handleOIDCCallback)

	// Onboarding routes (require auth but allow onboarding role)
	s.mux.Handle("POST /api/v1/auth/org/create", authMW(http.HandlerFunc(s.handleOrgCreate)))
	s.mux.Handle("POST /api/v1/auth/org/join", authMW(http.HandlerFunc(s.handleOrgJoin)))
	// Invite routes (org admin)
	s.mux.Handle("POST /api/v1/members/invite", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateInvite))))
	s.mux.Handle("POST /api/v1/members/invite-link", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateInviteLink))))

	// Notebook routes
	s.mux.Handle("POST /api/v1/notebooks", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateNotebook))))
	s.mux.Handle("GET /api/v1/notebooks", authMW(http.HandlerFunc(s.handleListNotebooks)))
	s.mux.Handle("GET /api/v1/notebooks/{id}", authMW(http.HandlerFunc(s.handleGetNotebook)))
	s.mux.Handle("DELETE /api/v1/notebooks/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteNotebook))))
	s.mux.Handle("PUT /api/v1/notebooks/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUpdateNotebook))))

	// Cell routes
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateCell))))
	s.mux.Handle("PUT /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUpdateCell))))
	s.mux.Handle("DELETE /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteCell))))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/execute", authMW(http.HandlerFunc(s.handleExecuteCell)))

	// Cell history routes
	s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/cells/{cell_id}/versions", authMW(http.HandlerFunc(s.handleListCellVersions)))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/versions/{version_id}/restore", authMW(RequireRole("editor")(http.HandlerFunc(s.handleRestoreCellVersion))))

	// Snapshot routes
	s.mux.Handle("POST /api/v1/notebooks/{id}/snapshots", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateSnapshot))))
	s.mux.Handle("GET /api/v1/notebooks/{id}/snapshots", authMW(http.HandlerFunc(s.handleListSnapshots)))
	s.mux.Handle("POST /api/v1/notebooks/{id}/snapshots/{snapshot_id}/restore", authMW(RequireRole("editor")(http.HandlerFunc(s.handleRestoreSnapshot))))

	// Schedule routes
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/schedules", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateSchedule))))
	s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/schedules", authMW(http.HandlerFunc(s.handleListSchedules)))
	s.mux.Handle("GET /api/v1/schedules/{id}", authMW(http.HandlerFunc(s.handleGetSchedule)))
	s.mux.Handle("DELETE /api/v1/schedules/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteSchedule))))
	s.mux.Handle("PUT /api/v1/schedules/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUpdateSchedule))))

	// Dashboard routes
	s.mux.Handle("POST /api/v1/dashboards", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateDashboard))))
	s.mux.Handle("GET /api/v1/dashboards", authMW(http.HandlerFunc(s.handleListDashboards)))
	s.mux.Handle("GET /api/v1/dashboards/{id}", authMW(http.HandlerFunc(s.handleGetDashboard)))
	s.mux.Handle("DELETE /api/v1/dashboards/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteDashboard))))
	s.mux.Handle("POST /api/v1/dashboards/{id}/widgets", authMW(RequireRole("editor")(http.HandlerFunc(s.handleAddWidget))))
	s.mux.Handle("DELETE /api/v1/dashboards/{id}/widgets/{widget_id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteWidget))))
	s.mux.Handle("POST /api/v1/dashboards/{id}/share", authMW(RequireRole("editor")(http.HandlerFunc(s.handleShareDashboard))))
	s.mux.HandleFunc("GET /api/v1/public/dashboards/{token}", s.handlePublicDashboard)

	// WebSocket routes
	s.mux.Handle("GET /api/v1/ws/notebooks/{id}", authMW(http.HandlerFunc(s.handleNotebookWS)))

	// Internal routes (called by Hocuspocus relay only)
	s.mux.HandleFunc("GET /internal/yjs/{notebook_id}", s.handleInternalYjsGet)
	s.mux.HandleFunc("PUT /internal/yjs/{notebook_id}", s.handleInternalYjsPut)
	s.mux.HandleFunc("GET /internal/auth/validate", s.handleInternalAuthValidate)

	// Attachment routes
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/attachments", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUploadAttachment))))
	s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/attachments", authMW(http.HandlerFunc(s.handleListAttachments)))
	s.mux.Handle("GET /api/v1/attachments/{id}", authMW(http.HandlerFunc(s.handleGetAttachment)))
	s.mux.Handle("DELETE /api/v1/attachments/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteAttachment))))

	// Connector routes
	s.mux.Handle("POST /api/v1/connectors", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateConnector))))
	s.mux.Handle("POST /api/v1/connectors/test", authMW(http.HandlerFunc(s.handleTestConnectorConfig)))
	s.mux.Handle("GET /api/v1/connectors", authMW(http.HandlerFunc(s.handleListConnectors)))
	s.mux.Handle("DELETE /api/v1/connectors/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteConnector))))
	s.mux.Handle("POST /api/v1/connectors/{id}/test", authMW(http.HandlerFunc(s.handleTestConnector)))
	s.mux.Handle("GET /api/v1/connectors/{id}/schema", authMW(http.HandlerFunc(s.handleConnectorSchema)))
	s.mux.Handle("GET /api/v1/connectors/{id}/databases", authMW(http.HandlerFunc(s.handleListConnectorDatabases)))

	// Template routes
	s.mux.Handle("POST /api/v1/templates", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateTemplate))))
	s.mux.Handle("GET /api/v1/templates", authMW(http.HandlerFunc(s.handleListTemplates)))
	s.mux.Handle("DELETE /api/v1/templates/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteTemplate))))

	// Platform admin routes
	s.mux.Handle("GET /api/v1/admin/orgs", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListOrgs))))
	s.mux.Handle("GET /api/v1/admin/users", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListUsers))))

	// Audit routes
	s.mux.Handle("GET /api/v1/audit", authMW(RequireRole("admin")(http.HandlerFunc(s.handleListAuditLogs))))

	// Member routes
	s.mux.Handle("GET /api/v1/members", authMW(http.HandlerFunc(s.handleListMembers)))
	s.mux.Handle("POST /api/v1/members", authMW(RequireRole("admin")(http.HandlerFunc(s.handleInviteMember))))
	s.mux.Handle("PUT /api/v1/members/{user_id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateMemberRole))))
	s.mux.Handle("DELETE /api/v1/members/{user_id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleRemoveMember))))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
