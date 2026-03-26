package models

import "time"

type Notebook struct {
	ID          string      `json:"id"`
	OrgID       string      `json:"org_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Parameters  []Parameter `json:"parameters"`
	CreatedBy   string      `json:"created_by"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type Parameter struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default string `json:"default"`
}

type Cell struct {
	ID          string    `json:"id"`
	NotebookID  string    `json:"notebook_id"`
	Position    int       `json:"position"`
	Type        CellType  `json:"type"`
	Language    string    `json:"language,omitempty"`
	ConnectorID string    `json:"connector_id,omitempty"`
	Source      string    `json:"source"`
	Outputs     []Output  `json:"outputs"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CellType string

const (
	CellTypeCode CellType = "code"
	CellTypeText CellType = "text"
)

type Output struct {
	Type   string      `json:"type"` // "table", "chart", "error"
	Data   interface{} `json:"data,omitempty"`
	Config interface{} `json:"config,omitempty"`
}

type Schedule struct {
	ID                 string            `json:"id"`
	NotebookID         string            `json:"notebook_id"`
	CronExpression     string            `json:"cron_expression"`
	ParameterOverrides map[string]string `json:"parameter_overrides"`
	Enabled            bool              `json:"enabled"`
	LastRunAt          *time.Time        `json:"last_run_at,omitempty"`
	NextRunAt          *time.Time        `json:"next_run_at,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}
