package api

import (
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/database"
)

type Server struct {
	db        *database.DB
	jwt       *auth.JWTIssuer
	audit     *audit.Logger
	masterKey []byte
	hub       *Hub
	mux       *http.ServeMux
}

func NewServer(db *database.DB, jwt *auth.JWTIssuer, auditLogger *audit.Logger, masterKey []byte) *Server {
	s := &Server{
		db:        db,
		jwt:       jwt,
		audit:     auditLogger,
		masterKey: masterKey,
		hub:       NewHub(),
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	authMW := AuthMiddleware(s.jwt)

	// Public routes
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)

	// Notebook routes
	s.mux.Handle("POST /api/v1/notebooks", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateNotebook))))
	s.mux.Handle("GET /api/v1/notebooks", authMW(http.HandlerFunc(s.handleListNotebooks)))
	s.mux.Handle("GET /api/v1/notebooks/{id}", authMW(http.HandlerFunc(s.handleGetNotebook)))
	s.mux.Handle("DELETE /api/v1/notebooks/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteNotebook))))

	// Cell routes
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateCell))))
	s.mux.Handle("PUT /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUpdateCell))))
	s.mux.Handle("DELETE /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteCell))))
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/execute", authMW(http.HandlerFunc(s.handleExecuteCell)))

	// Schedule routes
	s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/schedules", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateSchedule))))
	s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/schedules", authMW(http.HandlerFunc(s.handleListSchedules)))
	s.mux.Handle("GET /api/v1/schedules/{id}", authMW(http.HandlerFunc(s.handleGetSchedule)))
	s.mux.Handle("DELETE /api/v1/schedules/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteSchedule))))

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

	// Connector routes
	s.mux.Handle("POST /api/v1/connectors", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateConnector))))
	s.mux.Handle("GET /api/v1/connectors", authMW(http.HandlerFunc(s.handleListConnectors)))
	s.mux.Handle("DELETE /api/v1/connectors/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteConnector))))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
