package models

import "time"

type Connector struct {
	ID             string          `json:"id"`
	OrgID          string          `json:"org_id"`
	Name           string          `json:"name"`
	Type           ConnectorType   `json:"type"`
	Config         ConnectorConfig `json:"config"`
	MaxRows        int             `json:"max_rows"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	IsDefault      bool            `json:"is_default"`
	FolderID       *string         `json:"folder_id,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type ConnectorType string

const (
	ConnectorPostgres   ConnectorType = "postgres"
	ConnectorClickHouse ConnectorType = "clickhouse"
	ConnectorOpenSearch ConnectorType = "opensearch"
)

type ConnectorConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"ssl_mode,omitempty"`
}
