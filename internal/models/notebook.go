package models

import (
	"encoding/json"
	"time"
)

type Notebook struct {
	ID          string      `json:"id"`
	OrgID       string      `json:"org_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	ConnectorID string      `json:"connector_id,omitempty"`
	FolderID    *string     `json:"folder_id,omitempty"`
	Parameters  []Parameter `json:"parameters"`
	CreatedBy   string      `json:"created_by"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	DeletedAt   *time.Time  `json:"deleted_at,omitempty"`
}

type Parameter struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default string `json:"default"`
}

type Cell struct {
	ID             string          `json:"id"`
	NotebookID     string          `json:"notebook_id"`
	Position       int             `json:"position"`
	Type           CellType        `json:"type"`
	Language       string          `json:"language,omitempty"`
	ConnectorID    string          `json:"connector_id,omitempty"`
	Source         string          `json:"source"`
	Outputs        []Output        `json:"outputs"`
	SourceVisible  bool            `json:"source_visible"`
	OutputsHidden  bool            `json:"outputs_hidden"`
	CellCollapsed  bool            `json:"cell_collapsed"`
	SlideBreak     bool            `json:"slide_break"`
	Parameters     []Parameter     `json:"parameters"`
	Title          string          `json:"title,omitempty"`
	Description    string          `json:"description,omitempty"`
	Slug           string          `json:"slug,omitempty"`
	Limit          *int            `json:"limit,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	AgentUpdatedAt *time.Time      `json:"agent_updated_at,omitempty"`
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

type CellVersion struct {
	ID        string    `json:"id"`
	CellID    string    `json:"cell_id"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	User      *User     `json:"user,omitempty"`
}

type SnapshotCell struct {
	ID            string          `json:"id"`
	Type          CellType        `json:"type"`
	Language      string          `json:"language,omitempty"`
	Source        string          `json:"source"`
	Position      int             `json:"position"`
	ConnectorID   string          `json:"connector_id,omitempty"`
	Outputs       json.RawMessage `json:"outputs,omitempty"`
	Limit         *int            `json:"limit,omitempty"`
	SourceVisible bool            `json:"source_visible"`
	CellCollapsed bool            `json:"cell_collapsed"`
	SlideBreak    bool            `json:"slide_break"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	Title         string          `json:"title,omitempty"`
	Description   string          `json:"description,omitempty"`
}

type CellDiffLine struct {
	Type   string `json:"type"` // "add", "del", "ctx"
	Line   string `json:"line"`
	OldNum int    `json:"old_num,omitempty"`
	NewNum int    `json:"new_num,omitempty"`
}

type CellDiff struct {
	CellID    string         `json:"cell_id"`
	Position  int            `json:"position"`
	Title     string         `json:"title,omitempty"`
	OldSource string         `json:"old_source"`
	NewSource string         `json:"new_source"`
	DiffLines []CellDiffLine `json:"diff_lines"`
	Summary   string         `json:"summary"`
}

type CellChange struct {
	CellID      string `json:"cell_id"`
	Position    int    `json:"position"`
	OldPosition int    `json:"old_position,omitempty"`
	Title       string `json:"title,omitempty"`
}

type SnapshotChanges struct {
	TitleChanged     bool         `json:"title_changed"`
	OldTitle         string       `json:"old_title"`
	NewTitle         string       `json:"new_title"`
	CellsAdded       []CellChange `json:"cells_added"`
	CellsDeleted     []CellChange `json:"cells_deleted"`
	CellsModified    []CellChange `json:"cells_modified"`
	PositionsChanged []CellChange `json:"positions_changed"`
	CellDiffs        []CellDiff   `json:"cell_diffs,omitempty"`
}

type NotebookSnapshot struct {
	ID          string            `json:"id"`
	NotebookID  string            `json:"notebook_id"`
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	CellSources map[string]string `json:"cell_sources"`
	Cells       []SnapshotCell    `json:"cells,omitempty"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
	Auto        bool              `json:"auto"`
	User        *User             `json:"user,omitempty"`
	Changes     *SnapshotChanges  `json:"changes,omitempty"`
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
