package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/models"
)

func TestAllBuiltinToolsHaveTimeout(t *testing.T) {
	db := setupEngineTestDB(t)
	engine := newTestEngine(db)

	seen := map[string]bool{}
	for _, def := range engine.registry.List() {
		switch def.Function.Name {
		case "ask_question", "spawn_subagents":
			require.Equal(t, NoTimeout, def.Timeout, def.Function.Name)
			seen[def.Function.Name] = true
		default:
			require.Positive(t, def.Timeout, "tool %s must declare a positive timeout", def.Function.Name)
		}
	}
	require.True(t, seen["ask_question"] && seen["spawn_subagents"], "interactive tools missing from registry")

	for _, name := range []string{"run_cell", "create_cell"} {
		def, ok := engine.registry.Get(name)
		require.True(t, ok, "registry must contain %s", name)
		require.NotNil(t, def.TimeoutFromArgs, "%s must parse timeout_ms from args", name)
	}
}

func TestDynamicToolDefsDeclareTimeout(t *testing.T) {
	sqlDef, err := makeSQLQueryToolDef(&models.Tool{
		Name:   "sql_query",
		Config: models.JSONMap{"connector_id": "connector-1", "query": "SELECT 1"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, sqlDef.Timeout)

	webhookDef, err := makeWebhookToolDef(&models.Tool{
		Name:   "notify",
		Config: models.JSONMap{"url": "https://example.com/hook"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, webhookDef.Timeout)
}

func TestMCPToolDefsDeclareTimeout(t *testing.T) {
	handler := func(json.RawMessage, *ToolContext) (any, error) { return nil, nil }

	listDef := mcpListToolDef("weather", handler)
	require.Equal(t, "weather_list_tools", listDef.Function.Name)
	require.Equal(t, 30*time.Second, listDef.Timeout)

	callDef := mcpCallToolDef("weather", handler)
	require.Equal(t, "weather_call_tool", callDef.Function.Name)
	require.Equal(t, 60*time.Second, callDef.Timeout)
}

func TestTimeoutMsFromArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		want time.Duration
		ok   bool
	}{
		{"absent", `{}`, 0, false},
		{"zero", `{"timeout_ms":0}`, 0, false},
		{"negative", `{"timeout_ms":-1}`, 0, false},
		{"positive", `{"timeout_ms":1500}`, 1500 * time.Millisecond, true},
		{"clamped", `{"timeout_ms":900000}`, maxToolTimeoutMs * time.Millisecond, true},
		{"invalid json", `not-json`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := timeoutMsFromArgs(json.RawMessage(tc.args))
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestCreateCellTimeoutFromArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		want time.Duration
		ok   bool
	}{
		{"run absent", `{"timeout_ms":1500}`, 0, false},
		{"run false", `{"run":false,"timeout_ms":1500}`, 0, false},
		{"run true", `{"run":true,"timeout_ms":1500}`, 1500 * time.Millisecond, true},
		{"run true clamped", `{"run":true,"timeout_ms":900000}`, maxToolTimeoutMs * time.Millisecond, true},
		{"run true no timeout", `{"run":true}`, 0, false},
		{"invalid json", `not-json`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := createCellTimeoutFromArgs(json.RawMessage(tc.args))
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}
