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
	mux       *http.ServeMux
}

func NewServer(db *database.DB, jwt *auth.JWTIssuer, auditLogger *audit.Logger, masterKey []byte) *Server {
	s := &Server{
		db:        db,
		jwt:       jwt,
		audit:     auditLogger,
		masterKey: masterKey,
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

	// Connector routes
	s.mux.Handle("POST /api/v1/connectors", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateConnector))))
	s.mux.Handle("GET /api/v1/connectors", authMW(http.HandlerFunc(s.handleListConnectors)))
	s.mux.Handle("DELETE /api/v1/connectors/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteConnector))))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
