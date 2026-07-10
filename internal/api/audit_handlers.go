package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/the-heaven-labs/aether/internal/audit"
)

type auditListResponse struct {
	Entries []audit.Entry `json:"entries"`
	Total   int           `json:"total"`
}

type auditS3ConfigRequest struct {
	Endpoint          string `json:"endpoint"`
	Region            string `json:"region"`
	Bucket            string `json:"bucket"`
	AccessKey         string `json:"access_key"`
	SecretKey         string `json:"secret_key"`
	UseRole           bool   `json:"use_role"`
	BatchSize         int    `json:"batch_size"`
	FlushIntervalSecs int    `json:"flush_interval_secs"`
	Enabled           bool   `json:"enabled"`
}

type auditS3ConfigResponse struct {
	Endpoint          string `json:"endpoint"`
	Region            string `json:"region"`
	Bucket            string `json:"bucket"`
	UseRole           bool   `json:"use_role"`
	BatchSize         int    `json:"batch_size"`
	FlushIntervalSecs int    `json:"flush_interval_secs"`
	Enabled           bool   `json:"enabled"`
}

// @Summary List audit logs
// @Description Returns audit log entries for the organization
// @Tags audit
// @Produce json
// @Param limit query int false "Maximum number of entries (max 500)"
// @Param offset query int false "Number of entries to skip"
// @Param action query string false "Filter by action"
// @Param user_id query string false "Filter by user ID"
// @Param user query string false "Filter by user email"
// @Param resource_type query string false "Filter by resource type"
// @Param resource_id query string false "Filter by resource ID"
// @Param from query string false "Filter by start date"
// @Param to query string false "Filter by end date"
// @Success 200 {object} auditListResponse
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/audit [get]
func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	q := r.URL.Query()

	limit := 100
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	params := audit.QueryParams{
		OrgID:        claims.OrgID,
		Limit:        limit,
		Offset:       offset,
		Action:       q.Get("action"),
		UserID:       q.Get("user_id"),
		UserEmail:    q.Get("user"),
		ResourceType: q.Get("resource_type"),
		ResourceID:   q.Get("resource_id"),
		DateFrom:     q.Get("from"),
		DateTo:       q.Get("to"),
	}

	entries, err := s.audit.Query(ctx, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if entries == nil {
		entries = []audit.Entry{}
	}

	total, err := s.audit.Count(ctx, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "count failed")
		return
	}

	writeJSON(w, http.StatusOK, auditListResponse{
		Entries: entries,
		Total:   total,
	})
}

// ─── Platform-level handlers (platform admin) ────────────────────────────────

func (s *Server) handlePlatformGetAuditS3Config(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var endpoint, region, bucket string
	var useRole bool
	var batchSize, flushIntervalSecs int
	var enabled bool

	err := s.db.Pool.QueryRow(ctx,
		`SELECT endpoint, region, bucket, use_role, batch_size, flush_interval_secs, enabled
		 FROM platform_audit_s3_config LIMIT 1`,
	).Scan(&endpoint, &region, &bucket, &useRole, &batchSize, &flushIntervalSecs, &enabled)
	if err != nil {
		writeJSON(w, http.StatusOK, auditS3ConfigResponse{})
		return
	}

	writeJSON(w, http.StatusOK, auditS3ConfigResponse{
		Endpoint: endpoint, Region: region, Bucket: bucket,
		UseRole: useRole, BatchSize: batchSize, FlushIntervalSecs: flushIntervalSecs, Enabled: enabled,
	})
}

func (s *Server) handlePlatformUpdateAuditS3Config(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req auditS3ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Region == "" {
		req.Region = "us-east-1"
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 100
	}
	if req.FlushIntervalSecs <= 0 {
		req.FlushIntervalSecs = 60
	}

	var existingID int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id FROM platform_audit_s3_config LIMIT 1`,
	).Scan(&existingID)

	if err != nil {
		_, err = s.db.Pool.Exec(ctx,
			`INSERT INTO platform_audit_s3_config (endpoint, region, bucket, access_key, secret_key, use_role, batch_size, flush_interval_secs, enabled, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())`,
			req.Endpoint, req.Region, req.Bucket, req.AccessKey, req.SecretKey,
			req.UseRole, req.BatchSize, req.FlushIntervalSecs, req.Enabled,
		)
	} else {
		_, err = s.db.Pool.Exec(ctx,
			`UPDATE platform_audit_s3_config SET
				endpoint=$1, region=$2, bucket=$3,
				access_key=CASE WHEN $4 = '' THEN access_key ELSE $4 END,
				secret_key=CASE WHEN $5 = '' THEN secret_key ELSE $5 END,
				use_role=$6, batch_size=$7, flush_interval_secs=$8, enabled=$9, updated_at=NOW()`,
			req.Endpoint, req.Region, req.Bucket, req.AccessKey, req.SecretKey,
			req.UseRole, req.BatchSize, req.FlushIntervalSecs, req.Enabled,
		)
	}
	if err != nil {
		slog.Error("failed to update platform audit s3 config", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update config")
		return
	}

	if err := s.reloadPlatformAuditS3Writer(ctx); err != nil {
		slog.Error("failed to reload platform audit s3 writer", "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePlatformTestAuditS3Config(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var endpoint, region, bucket, accessKey, secretKey string
	var useRole bool
	var batchSize, flushIntervalSecs int

	err := s.db.Pool.QueryRow(ctx,
		`SELECT endpoint, region, bucket, access_key, secret_key, use_role, batch_size, flush_interval_secs
		 FROM platform_audit_s3_config LIMIT 1`,
	).Scan(&endpoint, &region, &bucket, &accessKey, &secretKey, &useRole, &batchSize, &flushIntervalSecs)
	if err != nil {
		writeError(w, http.StatusNotFound, "no s3 config found")
		return
	}

	writer, err := audit.NewS3Writer(audit.S3Config{
		Endpoint: endpoint, Region: region, Bucket: bucket,
		AccessKey: accessKey, SecretKey: secretKey, UseRole: useRole,
		BatchSize: batchSize, FlushIntervalSecs: flushIntervalSecs,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create s3 client: "+err.Error())
		return
	}
	defer writer.Stop()

	if err := writer.TestConnection(ctx); err != nil {
		writeError(w, http.StatusBadRequest, "connection test failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── Org-level handlers (org admin) ─────────────────────────────────────────

func (s *Server) handleOrgGetAuditS3Config(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var endpoint, region, bucket string
	var useRole bool
	var batchSize, flushIntervalSecs int
	var enabled bool

	err := s.db.Pool.QueryRow(ctx,
		`SELECT endpoint, region, bucket, use_role, batch_size, flush_interval_secs, enabled
		 FROM audit_s3_config WHERE org_id=$1`, claims.OrgID,
	).Scan(&endpoint, &region, &bucket, &useRole, &batchSize, &flushIntervalSecs, &enabled)
	if err != nil {
		writeJSON(w, http.StatusOK, auditS3ConfigResponse{})
		return
	}

	writeJSON(w, http.StatusOK, auditS3ConfigResponse{
		Endpoint: endpoint, Region: region, Bucket: bucket,
		UseRole: useRole, BatchSize: batchSize, FlushIntervalSecs: flushIntervalSecs, Enabled: enabled,
	})
}

func (s *Server) handleOrgUpdateAuditS3Config(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var req auditS3ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Region == "" {
		req.Region = "us-east-1"
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 100
	}
	if req.FlushIntervalSecs <= 0 {
		req.FlushIntervalSecs = 60
	}

	var existingID string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id FROM audit_s3_config WHERE org_id=$1`, claims.OrgID,
	).Scan(&existingID)

	if err != nil {
		_, err = s.db.Pool.Exec(ctx,
			`INSERT INTO audit_s3_config (org_id, endpoint, region, bucket, access_key, secret_key, use_role, batch_size, flush_interval_secs, enabled, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())`,
			claims.OrgID, req.Endpoint, req.Region, req.Bucket, req.AccessKey, req.SecretKey,
			req.UseRole, req.BatchSize, req.FlushIntervalSecs, req.Enabled,
		)
	} else {
		_, err = s.db.Pool.Exec(ctx,
			`UPDATE audit_s3_config SET
				endpoint=$1, region=$2, bucket=$3,
				access_key=CASE WHEN $4 = '' THEN access_key ELSE $4 END,
				secret_key=CASE WHEN $5 = '' THEN secret_key ELSE $5 END,
				use_role=$6, batch_size=$7, flush_interval_secs=$8, enabled=$9, updated_at=NOW()
			 WHERE org_id=$10`,
			req.Endpoint, req.Region, req.Bucket, req.AccessKey, req.SecretKey,
			req.UseRole, req.BatchSize, req.FlushIntervalSecs, req.Enabled, claims.OrgID,
		)
	}
	if err != nil {
		slog.Error("failed to update org audit s3 config", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update config")
		return
	}

	if err := s.reloadOrgAuditS3Writer(ctx, claims.OrgID); err != nil {
		slog.Error("failed to reload org audit s3 writer", "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleOrgTestAuditS3Config(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var endpoint, region, bucket, accessKey, secretKey string
	var useRole bool
	var batchSize, flushIntervalSecs int

	err := s.db.Pool.QueryRow(ctx,
		`SELECT endpoint, region, bucket, access_key, secret_key, use_role, batch_size, flush_interval_secs
		 FROM audit_s3_config WHERE org_id=$1`, claims.OrgID,
	).Scan(&endpoint, &region, &bucket, &accessKey, &secretKey, &useRole, &batchSize, &flushIntervalSecs)
	if err != nil {
		writeError(w, http.StatusNotFound, "no s3 config found")
		return
	}

	writer, err := audit.NewS3Writer(audit.S3Config{
		Endpoint: endpoint, Region: region, Bucket: bucket,
		AccessKey: accessKey, SecretKey: secretKey, UseRole: useRole,
		BatchSize: batchSize, FlushIntervalSecs: flushIntervalSecs,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create s3 client: "+err.Error())
		return
	}
	defer writer.Stop()

	if err := writer.TestConnection(ctx); err != nil {
		writeError(w, http.StatusBadRequest, "connection test failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── Reload helpers ──────────────────────────────────────────────────────────

func (s *Server) reloadPlatformAuditS3Writer(ctx context.Context) error {
	var endpoint, region, bucket, accessKey, secretKey string
	var useRole bool
	var batchSize, flushIntervalSecs int
	var enabled bool

	err := s.db.Pool.QueryRow(ctx,
		`SELECT endpoint, region, bucket, access_key, secret_key, use_role, batch_size, flush_interval_secs, enabled
		 FROM platform_audit_s3_config LIMIT 1`,
	).Scan(&endpoint, &region, &bucket, &accessKey, &secretKey, &useRole, &batchSize, &flushIntervalSecs, &enabled)
	if err != nil || !enabled || bucket == "" {
		s.audit.StopPlatformS3Writer()
		return nil
	}

	writer, err := audit.NewS3Writer(audit.S3Config{
		Endpoint: endpoint, Region: region, Bucket: bucket,
		AccessKey: accessKey, SecretKey: secretKey, UseRole: useRole,
		BatchSize: batchSize, FlushIntervalSecs: flushIntervalSecs,
	})
	if err != nil {
		return err
	}

	s.audit.SetPlatformS3Writer(writer)
	return nil
}

func (s *Server) reloadOrgAuditS3Writer(ctx context.Context, orgID string) error {
	var endpoint, region, bucket, accessKey, secretKey string
	var useRole bool
	var batchSize, flushIntervalSecs int
	var enabled bool

	err := s.db.Pool.QueryRow(ctx,
		`SELECT endpoint, region, bucket, access_key, secret_key, use_role, batch_size, flush_interval_secs, enabled
		 FROM audit_s3_config WHERE org_id=$1`, orgID,
	).Scan(&endpoint, &region, &bucket, &accessKey, &secretKey, &useRole, &batchSize, &flushIntervalSecs, &enabled)
	if err != nil || !enabled || bucket == "" {
		s.audit.RemoveOrgS3Writer(orgID)
		return nil
	}

	writer, err := audit.NewS3Writer(audit.S3Config{
		Endpoint: endpoint, Region: region, Bucket: bucket,
		AccessKey: accessKey, SecretKey: secretKey, UseRole: useRole,
		BatchSize: batchSize, FlushIntervalSecs: flushIntervalSecs,
	})
	if err != nil {
		return err
	}

	s.audit.SetOrgS3Writer(orgID, writer)
	return nil
}

// StartAuditS3Writers loads all enabled audit S3 configs and starts their writers.
func (s *Server) StartAuditS3Writers(ctx context.Context) {
	if err := s.reloadPlatformAuditS3Writer(ctx); err != nil {
		slog.Error("failed to start platform audit s3 writer", "error", err)
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT org_id FROM audit_s3_config WHERE enabled = true AND bucket != ''`)
	if err != nil {
		slog.Error("failed to load org audit s3 configs", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var orgID string
		if err := rows.Scan(&orgID); err != nil {
			slog.Error("failed to scan org id", "error", err)
			continue
		}
		if err := s.reloadOrgAuditS3Writer(ctx, orgID); err != nil {
			slog.Error("failed to reload org audit s3 writer", "org", orgID, "error", err)
		}
	}
}
