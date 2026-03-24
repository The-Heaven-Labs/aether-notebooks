package api

import (
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/database"
)

type Server struct {
	db    *database.DB
	jwt   *auth.JWTIssuer
	audit *audit.Logger
	mux   *http.ServeMux
}

func NewServer(db *database.DB, jwt *auth.JWTIssuer, auditLogger *audit.Logger) *Server {
	s := &Server{
		db:    db,
		jwt:   jwt,
		audit: auditLogger,
		mux:   http.NewServeMux(),
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

	// Protected routes — these will be added in subsequent tasks
	_ = authMW // used when registering protected routes
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
