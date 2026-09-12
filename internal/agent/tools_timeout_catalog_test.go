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

	noTimeout := map[string]bool{"ask_question": true, "spawn_subagents": true}
	for _, def := range engine.registry.List() {
		if noTimeout[def.Function.Name] {
			require.Equal(t, NoTimeout, def.Timeout, def.Function.Name)
			continue
		}
		require.NotZero(t, def.Timeout, "tool %s must declare a timeout", def.Function.Name)
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
		{"clamped", `{"timeout_ms":900000}`, 600000 * time.Millisecond, true},
		{"invalid json", `not-json`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := timeoutMsFromArgs(json.RawMessage(tc.args))
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}
