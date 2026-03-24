package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/heavenlabs/hnb/internal/database"
)

type Entry struct {
	ID           int64                  `json:"id"`
	OrgID        string                 `json:"org_id"`
	UserID       string                 `json:"user_id,omitempty"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

type QueryParams struct {
	OrgID        string
	UserID       string
	Action       string
	ResourceType string
	Limit        int
	Offset       int
}

type Logger struct {
	db *database.DB
}

func NewLogger(db *database.DB) *Logger {
	return &Logger{db: db}
}

func (l *Logger) Log(ctx context.Context, e Entry) error {
	metadata := e.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = l.db.Pool.Exec(ctx,
		`INSERT INTO audit_logs (org_id, user_id, action, resource_type, resource_id, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		e.OrgID, nilIfEmpty(e.UserID), e.Action, e.ResourceType, nilIfEmpty(e.ResourceID), metaJSON,
	)
	return err
}

func (l *Logger) Query(ctx context.Context, p QueryParams) ([]Entry, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}

	query := `SELECT id, org_id, COALESCE(user_id::text, ''), action, resource_type,
	          COALESCE(resource_id::text, ''), metadata, created_at
	          FROM audit_logs WHERE org_id = $1`
	args := []interface{}{p.OrgID}
	argN := 2

	if p.UserID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argN)
		args = append(args, p.UserID)
		argN++
	}
	if p.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", argN)
		args = append(args, p.Action)
		argN++
	}
	if p.ResourceType != "" {
		query += fmt.Sprintf(" AND resource_type = $%d", argN)
		args = append(args, p.ResourceType)
		argN++
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)
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
		if err := rows.Scan(&e.ID, &e.OrgID, &e.UserID, &e.Action, &e.ResourceType,
			&e.ResourceID, &metaJSON, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &e.Metadata)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
