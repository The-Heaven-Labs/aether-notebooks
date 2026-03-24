package models

import "time"

type Org struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Slug      string      `json:"slug"`
	Settings  OrgSettings `json:"settings"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type OrgSettings struct {
	DefaultQueryTimeout int `json:"default_query_timeout_seconds,omitempty"`
	DefaultMaxRows      int `json:"default_max_rows,omitempty"`
	AuditRetentionDays  int `json:"audit_retention_days,omitempty"`
}

type OrgMember struct {
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)
