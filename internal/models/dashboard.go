package models

import "time"

type Dashboard struct {
	ID          string            `json:"id"`
	OrgID       string            `json:"org_id"`
	Title       string            `json:"title"`
	Settings    DashboardSettings `json:"settings"`
	PublicToken string            `json:"public_token,omitempty"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type DashboardSettings struct {
	AutoRefreshSeconds int               `json:"auto_refresh_seconds,omitempty"`
	ParameterOverrides map[string]string `json:"parameter_overrides,omitempty"`
}

type Widget struct {
	ID          string                 `json:"id"`
	DashboardID string                 `json:"dashboard_id"`
	NotebookID  string                 `json:"notebook_id"`
	CellID      string                 `json:"cell_id"`
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
