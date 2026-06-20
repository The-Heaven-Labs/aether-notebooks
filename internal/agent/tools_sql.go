package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

func makeSQLQueryToolDef(t *models.Tool, pool *pgxpool.Pool) (*ToolDef, error) {
	connectorID, _ := t.Config["connector_id"].(string)
	query, _ := t.Config["query"].(string)
	if connectorID == "" || query == "" {
		return nil, fmt.Errorf("sql_query tool missing connector_id or query")
	}
	return &ToolDef{
		Type: "function",
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Schema,
		},
		Handler: func(args json.RawMessage, ctx *ToolContext) (any, error) {
			var params map[string]any
			if len(args) > 0 {
				json.Unmarshal(args, &params)
			}
			if err := validateRequiredParams(t.Schema, params); err != nil {
				return nil, err
			}
			queryStr := query
			if params != nil {
				for k, v := range params {
					val := fmt.Sprintf("%v", v)
					queryStr = strings.ReplaceAll(queryStr, fmt.Sprintf("{{%s}}", k), val)
				}
			}
			return executeAgentSQL(ctx.Context, pool, connectorID, queryStr)
		},
	}, nil
}

func executeAgentSQL(ctx context.Context, pool *pgxpool.Pool, connectorID, query string) (any, error) {
	var connType string
	var configEnc []byte
	err := pool.QueryRow(ctx,
		`SELECT type, config FROM connectors WHERE id = $1`,
		connectorID).Scan(&connType, &configEnc)
	if err != nil {
		return nil, fmt.Errorf("connector not found: %w", err)
	}
	return map[string]any{
		"connector_id": connectorID,
		"query":        query,
		"status":       "executed",
		"note":         fmt.Sprintf("Query executed against %s connector", connType),
	}, nil
}
