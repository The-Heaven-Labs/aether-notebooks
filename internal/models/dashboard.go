package models

import "time"

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

type DashboardSettings struct {
	AutoRefreshSeconds int                 `json:"auto_refresh_seconds,omitempty"`
	GridCols           int                 `json:"grid_cols,omitempty"`
	QueryCacheSeconds  *int                `json:"query_cache_seconds,omitempty"`
	PublicLive         bool                `json:"public_live,omitempty"`
	Variables          []DashboardVariable `json:"variables,omitempty"`
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
)

type WidgetLayout struct {
	Row    int `json:"row"`
	Col    int `json:"col"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
