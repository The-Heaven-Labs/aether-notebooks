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
	DefaultQueryTimeout int          `json:"default_query_timeout_seconds,omitempty"`
	DefaultMaxRows      int          `json:"default_max_rows,omitempty"`
	AuditRetentionDays  int          `json:"audit_retention_days,omitempty"`
	Branding            *OrgBranding `json:"branding,omitempty"`
	// HTMLPanelsEnabled gates the `html` widget type org-wide. nil (absent)
	// means enabled; an explicit false hides every panel behind a placeholder
	// and rejects creation/conversion.
	HTMLPanelsEnabled *bool `json:"html_panels_enabled,omitempty"`
}

// OrgBranding is the org-level theming that app surfaces fall back to when the
// dashboard has no (or a partial) app theme. Every field is optional; absent
// values keep the built-in defaults. LogoAttachmentID references an
// org-scoped attachment (POST /org/attachments).
type OrgBranding struct {
	AppName             string   `json:"app_name,omitempty"`
	LogoAttachmentID    string   `json:"logo_attachment_id,omitempty"`
	AccentColor         string   `json:"accent_color,omitempty"`
	DefaultChartPalette []string `json:"default_chart_palette,omitempty"`
}

type OrgMember struct {
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleEditor   Role = "editor"
	RoleViewer   Role = "viewer"
	RoleNoAccess Role = "no_access"
)
