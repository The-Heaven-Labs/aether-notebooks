package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
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
		ConfirmRequired: t.RequireConfirmation,
		Handler: func(args json.RawMessage, ctx *ToolContext) (any, error) {
			var llmParams map[string]any
			if len(args) > 0 {
				json.Unmarshal(args, &llmParams)
			}
			if err := validateRequiredParams(t.Schema, llmParams); err != nil {
				return nil, err
			}

			if err := ctx.CheckPermission("connector", connectorID, "use"); err != nil {
				return nil, err
			}

			if !isReadOnlyQuery(query) {
				return nil, fmt.Errorf("only read-only queries (SELECT, SHOW, DESCRIBE, EXPLAIN) are allowed")
			}

			// Convert LLM params to executor string map
			strParams := make(map[string]string)
			if llmParams != nil {
				for k, v := range llmParams {
					strParams[k] = fmt.Sprintf("%v", v)
				}
			}

			return executeAgentSQL(ctx.Context, pool, connectorID, query, strParams, ctx.MasterKey, ctx.OrgID)
		},
	}, nil
}

func executeAgentSQL(ctx context.Context, pool *pgxpool.Pool, connectorID, query string, params map[string]string, masterKey []byte, orgID string) (any, error) {
	var connType string
	var configEnc []byte
	err := pool.QueryRow(ctx,
		`SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
		connectorID, orgID).Scan(&connType, &configEnc)
	if err != nil {
		return nil, fmt.Errorf("connector not found: %w", err)
	}

	plain, err := crypto.Decrypt(configEnc, masterKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt connector config: %w", err)
	}

	driver, ok := executor.GetDriver(models.ConnectorType(connType))
	if !ok {
		return nil, fmt.Errorf("unsupported connector type: %s", connType)
	}

	exec, err := driver.NewExecutor(plain)
	if err != nil {
		return nil, fmt.Errorf("create executor: %w", err)
	}
	defer exec.Close()

	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := exec.Execute(c, query, params, 1000)
	if err != nil {
		return nil, fmt.Errorf("execute: %w", err)
	}

	return result, nil
}

func makeExecuteSQLHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ConnectorID string `json:"connector_id"`
			Query       string `json:"query"`
			Limit       int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.ConnectorID == "" {
			return nil, fmt.Errorf("connector_id is required")
		}
		if req.Query == "" {
			return nil, fmt.Errorf("query is required")
		}

		if err := ctx.CheckPermission("connector", req.ConnectorID, "use"); err != nil {
			return nil, err
		}

		if req.Limit <= 0 {
			req.Limit = 1000
		}

		if !isReadOnlyQuery(req.Query) {
			return nil, fmt.Errorf("only read-only queries (SELECT, SHOW, DESCRIBE, EXPLAIN) are allowed")
		}

		result, err := executeAgentSQL(ctx.Context, pool, req.ConnectorID, req.Query, nil, ctx.MasterKey, ctx.OrgID)
		if err != nil {
			return nil, err
		}

		return result, nil
	}
}

// isReadOnlyQuery checks whether a SQL query is read-only by examining the first
// non-comment keyword. This is a best-effort guard, not a security boundary.
func isReadOnlyQuery(query string) bool {
	s := strings.TrimSpace(query)
	// Strip leading SQL comments (both -- and /* */ styles)
	for {
		if strings.HasPrefix(s, "--") {
			idx := strings.Index(s, "\n")
			if idx < 0 {
				return false
			}
			s = strings.TrimSpace(s[idx+1:])
			continue
		}
		if strings.HasPrefix(s, "/*") {
			idx := strings.Index(s, "*/")
			if idx < 0 {
				return false
			}
			s = strings.TrimSpace(s[idx+2:])
			continue
		}
		break
	}

	// Extract the first word
	firstWord := ""
	for _, r := range s {
		if unicode.IsSpace(r) || r == '(' {
			break
		}
		firstWord += string(unicode.ToUpper(r))
	}

	switch firstWord {
	case "SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "WITH":
		return true
	default:
		return false
	}
}
