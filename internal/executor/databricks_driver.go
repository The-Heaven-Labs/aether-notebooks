package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/the-heaven-labs/aether/internal/models"
)

// databricksConfig is the typed config for the Databricks connector.
type databricksConfig struct {
	Host         string `json:"host"`
	HTTPPath     string `json:"http_path"`
	AuthType     string `json:"auth_type"`
	Token        string `json:"token"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Catalog      string `json:"catalog"`
	Schema       string `json:"schema"`
	Port         int    `json:"port,omitempty"`
}

// DatabricksDriver implements ConnectorDriver for Databricks SQL.
type DatabricksDriver struct{}

func init() {
	RegisterDriver(&DatabricksDriver{})
}

func (d *DatabricksDriver) Type() models.ConnectorType {
	return models.ConnectorDatabricks
}

func (d *DatabricksDriver) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "host", Type: "string", Required: true, Description: "Workspace hostname, e.g. dbc-abc123.cloud.databricks.com"},
			{Name: "http_path", Type: "string", Required: true, Description: "Warehouse connection path from the workspace's Connection details, e.g. /sql/1.0/warehouses/abc123"},
			{Name: "auth_type", Type: "string", Required: true, Default: "pat", Description: "Authentication type: pat or oauth_m2m"},
			{Name: "token", Type: "string", Required: false, Secret: true, Description: "Personal access token (auth_type=pat)"},
			{Name: "client_id", Type: "string", Required: false, Description: "Service principal client ID (auth_type=oauth_m2m)"},
			{Name: "client_secret", Type: "string", Required: false, Secret: true, Description: "Service principal OAuth secret (auth_type=oauth_m2m)"},
			{Name: "catalog", Type: "string", Required: false, Description: "Initial catalog (optional)"},
			{Name: "schema", Type: "string", Required: false, Description: "Initial schema (optional)"},
		},
	}
}

func (d *DatabricksDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error) {
	var cfg databricksConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("invalid databricks config: %w", err)
	}
	cfg, err := validateDatabricksConfig(cfg)
	if err != nil {
		return nil, err
	}
	return NewDatabricksExecutor(cfg)
}

func (d *DatabricksDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error {
	exec, err := d.NewExecutor(rawConfig)
	if err != nil {
		return err
	}
	defer exec.Close()
	return exec.TestConnection(ctx)
}
