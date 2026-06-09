package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/heavenlabs/hnb/internal/models"
)

// clickhouseConfig is the typed config for the ClickHouse connector.
type clickhouseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

// ClickHouseDriver implements ConnectorDriver for ClickHouse.
type ClickHouseDriver struct{}

func (d *ClickHouseDriver) Type() models.ConnectorType {
	return models.ConnectorClickHouse
}

func (d *ClickHouseDriver) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "host", Type: "string", Required: true, Description: "ClickHouse host"},
			{Name: "port", Type: "int", Required: true, Default: 9000, Description: "Native protocol port"},
			{Name: "user", Type: "string", Required: true, Description: "ClickHouse user"},
			{Name: "password", Type: "string", Required: true, Description: "ClickHouse password"},
			{Name: "database", Type: "string", Required: true, Description: "Database name"},
		},
	}
}

func (d *ClickHouseDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error) {
	var cfg clickhouseConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("invalid clickhouse config: %w", err)
	}
	if cfg.Host == "" || cfg.Database == "" {
		return nil, fmt.Errorf("clickhouse config requires host and database")
	}
	connCfg := models.ConnectorConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		Database: cfg.Database,
	}
	return NewClickHouseExecutor(connCfg)
}

func (d *ClickHouseDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error {
	exec, err := d.NewExecutor(rawConfig)
	if err != nil {
		return err
	}
	defer exec.Close()
	return exec.TestConnection(ctx)
}
