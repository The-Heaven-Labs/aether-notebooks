package api

import (
	"encoding/json"
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/heavenlabs/hnb/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

type createScheduleRequest struct {
	CronExpression     string            `json:"cron_expression"`
	ParameterOverrides map[string]string `json:"parameter_overrides,omitempty"`
}

func (s *Server) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")

	var req createScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CronExpression == "" {
		writeError(w, http.StatusBadRequest, "cron_expression is required")
		return
	}

	nextRun, err := scheduler.NextRun(req.CronExpression)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cron expression")
		return
	}

	ctx := r.Context()

	// Verify notebook belongs to org
	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	if req.ParameterOverrides == nil {
		req.ParameterOverrides = map[string]string{}
	}
	overridesJSON, _ := json.Marshal(req.ParameterOverrides)

	var sched models.Schedule
	var overridesOut []byte
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO schedules (notebook_id, cron_expression, parameter_overrides, next_run_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, notebook_id, cron_expression, parameter_overrides, enabled, last_run_at, next_run_at, created_at, updated_at`,
		nbID, req.CronExpression, overridesJSON, nextRun,
	).Scan(&sched.ID, &sched.NotebookID, &sched.CronExpression, &overridesOut,
		&sched.Enabled, &sched.LastRunAt, &sched.NextRunAt, &sched.CreatedAt, &sched.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create schedule")
		return
	}
	json.Unmarshal(overridesOut, &sched.ParameterOverrides)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "schedule.create", ResourceType: "schedule", ResourceID: sched.ID,
	})

	writeJSON(w, http.StatusCreated, sched)
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	// Verify notebook belongs to org
	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, notebook_id, cron_expression, parameter_overrides, enabled, last_run_at, next_run_at, created_at, updated_at
		 FROM schedules WHERE notebook_id = $1 ORDER BY created_at DESC`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var schedules []models.Schedule
	for rows.Next() {
		var sched models.Schedule
		var overridesOut []byte
		if err := rows.Scan(&sched.ID, &sched.NotebookID, &sched.CronExpression, &overridesOut,
			&sched.Enabled, &sched.LastRunAt, &sched.NextRunAt, &sched.CreatedAt, &sched.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		json.Unmarshal(overridesOut, &sched.ParameterOverrides)
		schedules = append(schedules, sched)
	}
	if schedules == nil {
		schedules = []models.Schedule{}
	}
	writeJSON(w, http.StatusOK, schedules)
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	schedID := r.PathValue("id")
	ctx := r.Context()

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM schedules WHERE id = $1
		 AND notebook_id IN (SELECT id FROM notebooks WHERE org_id = $2)`,
		schedID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "schedule.delete", ResourceType: "schedule", ResourceID: schedID,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	schedID := r.PathValue("id")
	ctx := r.Context()

	var sched models.Schedule
	var overridesOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT s.id, s.notebook_id, s.cron_expression, s.parameter_overrides, s.enabled, s.last_run_at, s.next_run_at, s.created_at, s.updated_at
		 FROM schedules s
		 JOIN notebooks n ON n.id = s.notebook_id
		 WHERE s.id = $1 AND n.org_id = $2`,
		schedID, claims.OrgID,
	).Scan(&sched.ID, &sched.NotebookID, &sched.CronExpression, &overridesOut,
		&sched.Enabled, &sched.LastRunAt, &sched.NextRunAt, &sched.CreatedAt, &sched.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	json.Unmarshal(overridesOut, &sched.ParameterOverrides)
	writeJSON(w, http.StatusOK, sched)
}
