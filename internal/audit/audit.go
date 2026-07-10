// Package audit provides a ClickHouse-backed audit logging system for tracking user actions.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/the-heaven-labs/aether/internal/database"
)

type Entry struct {
	ID                 int64          `json:"id"`
	OrgID              string         `json:"org_id"`
	UserID             string         `json:"user_id,omitempty"`
	UserEmail          string         `json:"user_email,omitempty"`
	Action             string         `json:"action"`
	ResourceType       string         `json:"resource_type"`
	ResourceID         string         `json:"resource_id,omitempty"`
	ResourceName       string         `json:"resource_name,omitempty"`
	ResourceParentName string         `json:"resource_parent_name,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
}

type QueryParams struct {
	OrgID        string
	UserID       string
	UserEmail    string
	Action       string
	ResourceType string
	ResourceID   string
	DateFrom     string // RFC3339 or date string (YYYY-MM-DD)
	DateTo       string // RFC3339 or date string (YYYY-MM-DD)
	Limit        int
	Offset       int
}

type Logger struct {
	db             *database.DB
	platformWriter *S3Writer
	orgWriters     map[string]*S3Writer // orgID -> S3Writer
}

func NewLogger(db *database.DB) *Logger {
	return &Logger{db: db, orgWriters: make(map[string]*S3Writer)}
}

func (l *Logger) SetPlatformS3Writer(w *S3Writer) {
	if l.platformWriter != nil {
		l.platformWriter.Stop()
	}
	l.platformWriter = w
	if w != nil {
		w.Start()
	}
}

func (l *Logger) StopPlatformS3Writer() {
	l.SetPlatformS3Writer(nil)
}

func (l *Logger) SetOrgS3Writer(orgID string, w *S3Writer) {
	if old, ok := l.orgWriters[orgID]; ok {
		old.Stop()
	}
	l.orgWriters[orgID] = w
	if w != nil {
		w.Start()
	}
}

func (l *Logger) RemoveOrgS3Writer(orgID string) {
	if old, ok := l.orgWriters[orgID]; ok {
		old.Stop()
		delete(l.orgWriters, orgID)
	}
}

func (l *Logger) StopAllS3Writers() {
	if l.platformWriter != nil {
		l.platformWriter.Stop()
		l.platformWriter = nil
	}
	for _, w := range l.orgWriters {
		w.Stop()
	}
	l.orgWriters = make(map[string]*S3Writer)
}

func (l *Logger) Log(ctx context.Context, e Entry) error {
	metadata := e.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	if e.OrgID != "" {
		_, err = l.db.Pool.Exec(ctx,
			`INSERT INTO audit_logs (org_id, user_id, action, resource_type, resource_id, metadata)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			e.OrgID, nilIfEmpty(e.UserID), e.Action, e.ResourceType, nilIfEmpty(e.ResourceID), metaJSON,
		)
	}
	if err != nil {
		return err
	}

	e.CreatedAt = time.Now()
	if l.platformWriter != nil {
		l.platformWriter.Write(e)
	}
	if w, ok := l.orgWriters[e.OrgID]; ok && w != nil {
		w.Write(e)
	}

	return nil
}

func (l *Logger) Query(ctx context.Context, p QueryParams) ([]Entry, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}

	query := `
		SELECT
			al.id, al.org_id,
			COALESCE(al.user_id::text, ''),
			COALESCE(u.email, ''),
			al.action, al.resource_type,
			COALESCE(al.resource_id::text, ''),
			COALESCE(
				CASE al.resource_type
					WHEN 'notebook'  THEN (SELECT title FROM notebooks WHERE id = al.resource_id)
					WHEN 'dashboard' THEN (SELECT title FROM dashboards WHERE id = al.resource_id)
					WHEN 'connector' THEN (SELECT name  FROM connectors WHERE id = al.resource_id)
					WHEN 'user'      THEN (SELECT name  FROM users     WHERE id = al.resource_id)
					ELSE ''
				END, ''
			),
			COALESCE(
				CASE al.resource_type
					WHEN 'cell' THEN (
						SELECT n.title FROM cells c
						LEFT JOIN notebooks n ON n.id = c.notebook_id
						WHERE c.id = al.resource_id
					)
					ELSE ''
				END, ''
			),
			al.metadata, al.created_at
		FROM audit_logs al
		LEFT JOIN users u ON u.id = al.user_id
		WHERE al.org_id = $1`
	args := []any{p.OrgID}
	argN := 2

	if p.UserID != "" {
		query += fmt.Sprintf(" AND al.user_id = $%d", argN)
		args = append(args, p.UserID)
		argN++
	}
	if p.Action != "" {
		query += fmt.Sprintf(" AND al.action ILIKE $%d", argN)
		args = append(args, "%"+p.Action+"%")
		argN++
	}
	if p.ResourceType != "" {
		query += fmt.Sprintf(" AND al.resource_type = $%d", argN)
		args = append(args, p.ResourceType)
		argN++
	}
	if p.ResourceID != "" {
		query += fmt.Sprintf(" AND al.resource_id = $%d", argN)
		args = append(args, p.ResourceID)
		argN++
	}
	if p.UserEmail != "" {
		query += fmt.Sprintf(" AND u.email ILIKE $%d", argN)
		args = append(args, "%"+p.UserEmail+"%")
		argN++
	}
	if p.DateFrom != "" {
		query += fmt.Sprintf(" AND al.created_at >= $%d::timestamptz", argN)
		args = append(args, p.DateFrom)
		argN++
	}
	if p.DateTo != "" {
		query += fmt.Sprintf(" AND al.created_at <= $%d::timestamptz", argN)
		args = append(args, p.DateTo+"T23:59:59Z")
		argN++
	}

	query += fmt.Sprintf(" ORDER BY al.created_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)
	args = append(args, p.Limit, p.Offset)

	rows, err := l.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var metaJSON []byte
		if err := rows.Scan(&e.ID, &e.OrgID, &e.UserID, &e.UserEmail, &e.Action,
			&e.ResourceType, &e.ResourceID, &e.ResourceName, &e.ResourceParentName, &metaJSON, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &e.Metadata)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return entries, nil
}

// Count returns the total number of audit entries matching the given filters
// (ignoring Limit/Offset).
func (l *Logger) Count(ctx context.Context, p QueryParams) (int, error) {
	// Use JOINed version when we need user email or date filtering
	needsJoin := p.UserEmail != "" || p.DateFrom != "" || p.DateTo != ""

	var query string
	if needsJoin {
		query = `SELECT COUNT(*) FROM audit_logs al LEFT JOIN users u ON u.id = al.user_id WHERE al.org_id = $1`
	} else {
		query = `SELECT COUNT(*) FROM audit_logs WHERE org_id = $1`
	}

	prefix := ""
	if needsJoin {
		prefix = "al."
	}

	args := []any{p.OrgID}
	argN := 2

	if p.UserID != "" {
		query += fmt.Sprintf(" AND %suser_id = $%d", prefix, argN)
		args = append(args, p.UserID)
		argN++
	}
	if p.Action != "" {
		query += fmt.Sprintf(" AND %saction ILIKE $%d", prefix, argN)
		args = append(args, "%"+p.Action+"%")
		argN++
	}
	if p.ResourceType != "" {
		query += fmt.Sprintf(" AND %sresource_type = $%d", prefix, argN)
		args = append(args, p.ResourceType)
		argN++
	}
	if p.ResourceID != "" {
		query += fmt.Sprintf(" AND %sresource_id = $%d", prefix, argN)
		args = append(args, p.ResourceID)
		argN++
	}
	if p.UserEmail != "" {
		query += fmt.Sprintf(" AND u.email ILIKE $%d", argN)
		args = append(args, "%"+p.UserEmail+"%")
		argN++
	}
	if p.DateFrom != "" {
		query += fmt.Sprintf(" AND %screated_at >= $%d::timestamptz", prefix, argN)
		args = append(args, p.DateFrom)
		argN++
	}
	if p.DateTo != "" {
		query += fmt.Sprintf(" AND %screated_at <= $%d::timestamptz", prefix, argN)
		args = append(args, p.DateTo+"T23:59:59Z")
		argN++
	}

	var count int
	err := l.db.Pool.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}
	return count, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
