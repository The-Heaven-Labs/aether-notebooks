package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/executor"
	"github.com/heavenlabs/hnb/internal/models"
)

type createConnectorRequest struct {
	Name   string                 `json:"name"`
	Type   models.ConnectorType   `json:"type"`
	Config models.ConnectorConfig `json:"config"`
}

func (s *Server) handleCreateConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req createConnectorRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.Type == "" {
		writeError(w, http.StatusBadRequest, "name and type are required")
		return
	}
	if req.Type != models.ConnectorPostgres && req.Type != models.ConnectorClickHouse {
		writeError(w, http.StatusBadRequest, "type must be 'postgres' or 'clickhouse'")
		return
	}

	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid config")
		return
	}

	encrypted, err := crypto.Encrypt(configJSON, s.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt config")
		return
	}

	ctx := r.Context()
	var id, orgID, name string
	var connType models.ConnectorType
	var maxRows, timeout int
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO connectors (org_id, name, type, config_encrypted)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, org_id, name, type, max_rows, timeout_seconds`,
		claims.OrgID, req.Name, req.Type, encrypted,
	).Scan(&id, &orgID, &name, &connType, &maxRows, &timeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create connector")
		return
	}

	// Mask password in response
	req.Config.Password = "***"
	conn := models.Connector{
		ID: id, OrgID: orgID, Name: name, Type: connType,
		Config: req.Config, MaxRows: maxRows, TimeoutSeconds: timeout,
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "connector.create", ResourceType: "connector", ResourceID: id,
	})

	writeJSON(w, http.StatusCreated, conn)
}

func (s *Server) handleListConnectors(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, name, type, config_encrypted, max_rows, timeout_seconds, created_at, updated_at
		 FROM connectors WHERE org_id = $1 ORDER BY name ASC`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var connectors []models.Connector
	for rows.Next() {
		var c models.Connector
		var encryptedConfig []byte
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
			&c.MaxRows, &c.TimeoutSeconds, &c.CreatedAt, &c.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		// Decrypt and mask password
		if plain, err := crypto.Decrypt(encryptedConfig, s.masterKey); err == nil {
			json.Unmarshal(plain, &c.Config)
			c.Config.Password = "***"
		}
		connectors = append(connectors, c)
	}

	if connectors == nil {
		connectors = []models.Connector{}
	}

	writeJSON(w, http.StatusOK, connectors)
}

func (s *Server) handleDeleteConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	connID := r.PathValue("id")
	ctx := r.Context()

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM connectors WHERE id = $1 AND org_id = $2`,
		connID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "connector.delete", ResourceType: "connector", ResourceID: connID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// loadConnectorRow fetches the connector type and encrypted config, scoped to orgID.
// Returns pgx.ErrNoRows if not found, so callers can distinguish 404 from 500.
func (s *Server) loadConnectorRow(ctx context.Context, connID, orgID string) (models.ConnectorType, []byte, error) {
	var configEnc []byte
	var connType models.ConnectorType
	err := s.db.Pool.QueryRow(ctx,
		`SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
		connID, orgID,
	).Scan(&connType, &configEnc)
	return connType, configEnc, err
}

// buildExecutor decrypts connector config and constructs the appropriate executor.
func (s *Server) buildExecutor(connType models.ConnectorType, configEnc []byte) (executor.Executor, error) {
	plain, err := crypto.Decrypt(configEnc, s.masterKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	var cfg models.ConnectorConfig
	if err := json.Unmarshal(plain, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	switch connType {
	case models.ConnectorPostgres:
		return executor.NewPostgresExecutor(cfg)
	case models.ConnectorClickHouse:
		return executor.NewClickHouseExecutor(cfg)
	default:
		return nil, fmt.Errorf("unsupported connector type: %s", connType)
	}
}

// handleTestConnectorConfig tests a connection using raw config (before saving).
func (s *Server) handleTestConnectorConfig(w http.ResponseWriter, r *http.Request) {
	var req createConnectorRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "invalid request body"})
		return
	}

	var exec executor.Executor
	var err error
	switch req.Type {
	case models.ConnectorPostgres:
		exec, err = executor.NewPostgresExecutor(req.Config)
	case models.ConnectorClickHouse:
		exec, err = executor.NewClickHouseExecutor(req.Config)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "unsupported connector type"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer exec.Close()

	if err := exec.TestConnection(r.Context()); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTestConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	connID := r.PathValue("id")
	ctx := r.Context()

	connType, configEnc, err := s.loadConnectorRow(ctx, connID, claims.OrgID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "connector not found"})
		return
	}
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "failed to connect"})
		return
	}
	defer exec.Close()

	if err := exec.TestConnection(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "connection failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleConnectorSchema(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	connID := r.PathValue("id")
	ctx := r.Context()

	connType, configEnc, err := s.loadConnectorRow(ctx, connID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build connector")
		return
	}
	defer exec.Close()

	schema, err := exec.Schema(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "schema fetch failed")
		return
	}
	writeJSON(w, http.StatusOK, schema)
}
