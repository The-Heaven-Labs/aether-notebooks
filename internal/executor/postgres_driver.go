package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/heavenlabs/hnb/internal/models"
)

// postgresConfig is the typed config for the Postgres connector.
type postgresConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"ssl_mode,omitempty"`
}

// PostgresDriver implements ConnectorDriver for PostgreSQL.
type PostgresDriver struct{}

func (d *PostgresDriver) Type() models.ConnectorType {
	return models.ConnectorPostgres
}

func (d *PostgresDriver) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "host", Type: "string", Required: true, Description: "Database host"},
			{Name: "port", Type: "int", Required: true, Default: 5432, Description: "Database port"},
			{Name: "user", Type: "string", Required: true, Description: "Database user"},
			{Name: "password", Type: "string", Required: true, Description: "Database password"},
			{Name: "database", Type: "string", Required: true, Description: "Database name"},
			{Name: "ssl_mode", Type: "string", Required: false, Default: "disable", Description: "SSL mode (disable, require, verify-full)"},
		},
	}
}

func (d *PostgresDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error) {
	var cfg postgresConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("invalid postgres config: %w", err)
	}
	if cfg.Host == "" || cfg.Database == "" {
		return nil, fmt.Errorf("postgres config requires host and database")
	}
	connCfg := models.ConnectorConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		Database: cfg.Database,
		SSLMode:  cfg.SSLMode,
	}
	return NewPostgresExecutor(connCfg)
}

func (d *PostgresDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error {
	exec, err := d.NewExecutor(rawConfig)
	if err != nil {
		return err
	}
	defer exec.Close()
	return exec.TestConnection(ctx)
}
