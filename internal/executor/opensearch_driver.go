package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/heavenlabs/hnb/internal/models"
)

// OpenSearchDriver implements ConnectorDriver for OpenSearch.
type OpenSearchDriver struct{}

func (d *OpenSearchDriver) Type() models.ConnectorType {
	return models.ConnectorOpenSearch
}

func (d *OpenSearchDriver) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "host", Type: "string", Required: true, Description: "OpenSearch host"},
			{Name: "port", Type: "int", Required: false, Default: 9200, Description: "OpenSearch port"},
			{Name: "user", Type: "string", Required: false, Description: "Username (empty for unauthenticated)"},
			{Name: "password", Type: "string", Required: false, Description: "Password (empty for unauthenticated)"},
			{Name: "use_tls", Type: "bool", Required: false, Default: false, Description: "Use HTTPS"},
		},
	}
}

func (d *OpenSearchDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error) {
	var cfg opensearchConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("invalid opensearch config: %w", err)
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("opensearch host is required")
	}
	return NewOpenSearchExecutor(cfg), nil
}

func (d *OpenSearchDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error {
	exec, err := d.NewExecutor(rawConfig)
	if err != nil {
		return err
	}
	defer exec.Close()
	return exec.TestConnection(ctx)
}
