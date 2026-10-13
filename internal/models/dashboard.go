package models

import (
	"encoding/json"
	"time"
)

type Dashboard struct {
	ID        string            `json:"id"`
	OrgID     string            `json:"org_id"`
	Title     string            `json:"title"`
	Settings  DashboardSettings `json:"settings"`
	FolderID  *string           `json:"folder_id,omitempty"`
	CreatedBy string            `json:"created_by"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	DeletedAt *time.Time        `json:"deleted_at,omitempty"`
}

// DashboardVersion is a published, immutable snapshot of a dashboard's
// document content: the title, settings (with variables folded in as
// settings["variables"]) and full widget definitions as of the publish.
// ContentHash is the canonical hash of that content, compared against the
// draft to report unpublished changes. Widgets stays raw JSONB so the
// dashboarddoc widget shape is decoded by the API layer rather than
// duplicated here.
type DashboardVersion struct {
	ID          string          `json:"id"`
	DashboardID string          `json:"dashboard_id"`
	OrgID       string          `json:"org_id"`
	Version     int             `json:"version"`
	Title       string          `json:"title"`
	Settings    map[string]any  `json:"settings"`
	Widgets     json.RawMessage `json:"widgets"`
	ContentHash string          `json:"content_hash"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
}

type DashboardSettings struct {
	AutoRefreshSeconds int                   `json:"auto_refresh_seconds,omitempty"`
	GridCols           int                   `json:"grid_cols,omitempty"`
	QueryCacheSeconds  *int                  `json:"query_cache_seconds,omitempty"`
	PublicLive         bool                  `json:"public_live,omitempty"`
	Variables          []DashboardVariable   `json:"variables,omitempty"`
	App                *DashboardAppSettings `json:"app,omitempty"`
	Tabs               []DashboardTab        `json:"tabs,omitempty"`
}

// DashboardTab is a chapter of the App surface. Widgets point at a tab via
// widgets.layout.tab; an empty or unknown tab falls back to the first tab.
type DashboardTab struct {
	ID    string `json:"id"`    // ^[a-z0-9][a-z0-9-]*$, unique, <=64 chars
	Title string `json:"title"` // <=60 runes
}

// DashboardAppSettings configures the dashboard's App surface (the
// full-bleed, chrome-free viewer). All fields are optional; absent values
// keep today's viewer behavior (fixed width, header/refresh/freshness shown).
type DashboardAppSettings struct {
	Title            string             `json:"title,omitempty"`
	Subtitle         string             `json:"subtitle,omitempty"`
	LogoAttachmentID string             `json:"logo_attachment_id,omitempty"`
	Width            string             `json:"width,omitempty"`        // "fixed" | "full"; empty = fixed
	Layout           string             `json:"layout,omitempty"`       // "grid" | "custom"; empty = grid (wave 4)
	FilterScope      string             `json:"filter_scope,omitempty"` // "all" | "tab"; empty = "all"
	ShowHeader       *bool              `json:"show_header,omitempty"`
	ShowRefresh      *bool              `json:"show_refresh,omitempty"`
	ShowLastUpdated  *bool              `json:"show_last_updated,omitempty"`
	Theme            *DashboardAppTheme `json:"theme,omitempty"`
}

// DashboardAppTheme overrides the app surface's tokens. Every field is
// optional; absent values fall through to org branding, then defaults.
type DashboardAppTheme struct {
	Accent       string   `json:"accent,omitempty"`
	Background   string   `json:"background,omitempty"`
	Surface      string   `json:"surface,omitempty"`
	Text         string   `json:"text,omitempty"`
	Border       string   `json:"border,omitempty"`
	Radius       *int     `json:"radius,omitempty"`        // 0..24
	FontFamily   string   `json:"font_family,omitempty"`   // dm-sans | jetbrains-mono | system
	ChartPalette []string `json:"chart_palette,omitempty"` // <=12 hex colors
	ColorScheme  string   `json:"color_scheme,omitempty"`  // auto | light | dark
}

// DashboardVariable is a dashboard-level filter. Values are interpolated
// server-side into query widgets as {{name}} tokens.
type DashboardVariable struct {
	Name      string           `json:"name"`
	Label     string           `json:"label"`
	Type      string           `json:"type"` // text|number|boolean|date|date_range|single_select|multi_select
	Default   interface{}      `json:"default,omitempty"`
	Required  bool             `json:"required,omitempty"`
	Options   *VariableOptions `json:"options,omitempty"`
	DependsOn []string         `json:"depends_on,omitempty"`
}

type VariableOptions struct {
	Mode        string         `json:"mode"` // "static" | "query"
	Values      []OptionValue  `json:"values,omitempty"`
	Query       *VariableQuery `json:"query,omitempty"`
	LabelColumn string         `json:"label_column,omitempty"`
	ValueColumn string         `json:"value_column,omitempty"`
}

type OptionValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type VariableQuery struct {
	ConnectorID string `json:"connector_id"`
	SQL         string `json:"sql"`
}

type Widget struct {
	ID          string                 `json:"id"`
	DashboardID string                 `json:"dashboard_id"`
	NotebookID  *string                `json:"notebook_id,omitempty"`
	CellID      *string                `json:"cell_id,omitempty"`
	ConnectorID *string                `json:"connector_id,omitempty"`
	Query       *string                `json:"query,omitempty"`
	Language    string                 `json:"language"`
	Type        WidgetType             `json:"type"`
	Layout      WidgetLayout           `json:"layout"`
	Config      map[string]interface{} `json:"config"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type WidgetType string

const (
	WidgetChart  WidgetType = "chart"
	WidgetTable  WidgetType = "table"
	WidgetText   WidgetType = "text"
	WidgetMetric WidgetType = "metric"
	WidgetHTML   WidgetType = "html"
)

type WidgetLayout struct {
	Row    int    `json:"row"`
	Col    int    `json:"col"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Tab    string `json:"tab,omitempty"`
}
