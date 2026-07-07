package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/models"
	"github.com/the-heaven-labs/aether/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

type createScheduleRequest struct {
	CronExpression     string            `json:"cron_expression"`
	ParameterOverrides map[string]string `json:"parameter_overrides,omitempty"`
}

// @Summary Create a schedule
// @Description Create a new schedule for a notebook
// @Tags schedules
// @Accept json
// @Produce json
// @Param notebook_id path string true "Notebook ID"
// @Param request body object true "Schedule details"
// @Success 201 {object} object
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{notebook_id}/schedules [post]
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

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions to schedule this notebook")
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

// @Summary List schedules
// @Description List all schedules for a notebook
// @Tags schedules
// @Produce json
// @Param notebook_id path string true "Notebook ID"
// @Success 200 {array} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{notebook_id}/schedules [get]
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

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "view"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
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

// @Summary Delete a schedule
// @Description Delete a schedule
// @Tags schedules
// @Param id path string true "Schedule ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /schedules/{id} [delete]
func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	schedID := r.PathValue("id")
	ctx := r.Context()

	var nbID string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT notebook_id FROM schedules WHERE id = $1
		 AND notebook_id IN (SELECT id FROM notebooks WHERE org_id = $2)`,
		schedID, claims.OrgID,
	).Scan(&nbID)
	if err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	_, err = s.db.Pool.Exec(ctx,
		`DELETE FROM schedules WHERE id = $1 AND notebook_id IN (SELECT id FROM notebooks WHERE org_id = $2)`,
		schedID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "schedule.delete", ResourceType: "schedule", ResourceID: schedID,
	})

	w.WriteHeader(http.StatusNoContent)
}

type updateScheduleRequest struct {
	Enabled            *bool             `json:"enabled,omitempty"`
	CronExpression     *string           `json:"cron_expression,omitempty"`
	ParameterOverrides map[string]string `json:"parameter_overrides,omitempty"`
}

// @Summary Update a schedule
// @Description Update a schedule's cron expression, enabled status, or parameter overrides
// @Tags schedules
// @Accept json
// @Produce json
// @Param id path string true "Schedule ID"
// @Param request body object true "Schedule updates"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /schedules/{id} [put]
func (s *Server) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	schedID := r.PathValue("id")

	var req updateScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Enabled == nil && req.CronExpression == nil && req.ParameterOverrides == nil {
		writeError(w, http.StatusBadRequest, "at least one field must be provided")
		return
	}

	ctx := r.Context()

	var nbID string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT notebook_id FROM schedules WHERE id = $1
		 AND notebook_id IN (SELECT id FROM notebooks WHERE org_id = $2)`,
		schedID, claims.OrgID,
	).Scan(&nbID)
	if err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	query := "UPDATE schedules SET updated_at = NOW()"
	args := []any{}
	argN := 1

	if req.Enabled != nil {
		query += fmt.Sprintf(", enabled = $%d", argN)
		args = append(args, *req.Enabled)
		argN++
	}
	if req.CronExpression != nil {
		next, err := scheduler.NextRun(*req.CronExpression)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cron expression")
			return
		}
		query += fmt.Sprintf(", cron_expression = $%d, next_run_at = $%d", argN, argN+1)
		args = append(args, *req.CronExpression, next)
		argN += 2
	}
	if req.ParameterOverrides != nil {
		overridesJSON, _ := json.Marshal(req.ParameterOverrides)
		query += fmt.Sprintf(", parameter_overrides = $%d", argN)
		args = append(args, overridesJSON)
		argN++
	}

	query += fmt.Sprintf(
		` WHERE id = $%d`,
		argN,
	)
	args = append(args, schedID)
	query += " RETURNING id, notebook_id, cron_expression, parameter_overrides, enabled, last_run_at, next_run_at, created_at, updated_at"

	var sched models.Schedule
	var overridesOut []byte
	err = s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&sched.ID, &sched.NotebookID, &sched.CronExpression, &overridesOut,
		&sched.Enabled, &sched.LastRunAt, &sched.NextRunAt, &sched.CreatedAt, &sched.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	json.Unmarshal(overridesOut, &sched.ParameterOverrides)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "schedule.update", ResourceType: "schedule", ResourceID: sched.ID,
	})
	writeJSON(w, http.StatusOK, sched)
}

// @Summary Get a schedule
// @Description Get a schedule by ID
// @Tags schedules
// @Produce json
// @Param id path string true "Schedule ID"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /schedules/{id} [get]
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

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", sched.NotebookID, "view"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	json.Unmarshal(overridesOut, &sched.ParameterOverrides)
	writeJSON(w, http.StatusOK, sched)
}
