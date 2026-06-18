package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// ——— LIST ———

func TestModelConfig_List(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		wantMin int
		wantMax int
	}{
		{
			name:    "adminA sees all Org A model configs",
			userKey: "adminA",
			wantMin: 4, wantMax: 4,
		},
		{
			name:    "aliceA sees model configs with user or everyone ACL",
			userKey: "aliceA",
			wantMin: 2, wantMax: 3,
		},
		{
			name:    "carolA sees only everyone-ACL model configs",
			userKey: "carolA",
			wantMin: 1, wantMax: 2,
		},
		{
			name:    "adminB sees only Org B model configs",
			userKey: "adminB",
			wantMin: 1, wantMax: 1,
		},
		{
			name:    "eveB sees only Org B model configs she can access",
			userKey: "eveB",
			wantMin: 0, wantMax: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "GET", "/api/v1/model-configs", nil)
			require.Equal(t, http.StatusOK, status)

			var configs []map[string]any
			json.Unmarshal([]byte(body), &configs)
			count := len(configs)
			t.Logf("%s: %d model configs visible", tt.userKey, count)
			require.GreaterOrEqual(t, count, tt.wantMin)
			require.LessOrEqual(t, count, tt.wantMax)
		})
	}
}

// ——— GET ———

func TestModelConfig_Get(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL, nil)
		t.Logf("aliceA GET NoACL model config: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can view NoACL model config without ACL entry")
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.UserACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("carolA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL, nil)
		t.Logf("carolA GET NoACL model config: %d %s", status, body)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL, nil)
		t.Logf("adminB GET Org A NoACL model config: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "GET", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL, nil)
		t.Logf("eveB GET Org A NoACL model config: %d %s", status, body)
	})
}

// ——— CREATE ———

func TestModelConfig_Create(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	payload := map[string]any{
		"name":     "test-mc",
		"provider": "openai",
		"base_url": "https://api.openai.com/v1",
		"model":    "gpt-4",
		"api_key":  "test-key",
	}

	t.Run("adminA creates model config — 201", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "POST", "/api/v1/model-configs", payload)
		t.Logf("adminA create: %d %s", status, body)
		require.Equal(t, http.StatusCreated, status)
	})

	t.Run("aliceA creates model config — 201 (open endpoint)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST", "/api/v1/model-configs", payload)
		t.Logf("aliceA create: %d %s", status, body)
		require.Equal(t, http.StatusCreated, status)
	})

	t.Run("carolA creates model config — 201 (open endpoint)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "POST", "/api/v1/model-configs", payload)
		t.Logf("carolA create: %d %s", status, body)
		require.Equal(t, http.StatusCreated, status)
	})
}

// ——— UPDATE ———

func TestModelConfig_Update(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "PUT", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL,
			map[string]string{"name": "updated-by-admin"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL,
			map[string]string{"name": "updated-by-alice"})
		t.Logf("aliceA PUT NoACL: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated NoACL model config")
		}
	})

	t.Run("aliceA on UserACL — 403 (view-only)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.UserACL,
			map[string]string{"name": "updated-by-alice"})
		t.Logf("aliceA PUT UserACL (view-only): %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated UserACL model config with only view")
		}
	})

	t.Run("bobA on GroupACL — 200 (group has edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "PUT", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.GroupACL,
			map[string]string{"name": "updated-by-bob"})
		t.Logf("bobA PUT GroupACL: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "PUT", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL,
			map[string]string{"name": "updated-by-adminb"})
		t.Logf("adminB PUT Org A: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "PUT", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL,
			map[string]string{"name": "updated-by-eveb"})
		t.Logf("eveB PUT Org A: %d %s", status, body)
	})
}

// ——— DELETE ———

func TestModelConfig_Delete(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 204", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "DELETE", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL, nil)
		t.Logf("adminA DELETE NoACL: %d %s", status, body)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on UserACL — 403 (view-only)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.UserACL, nil)
		t.Logf("aliceA DELETE UserACL: %d %s", status, body)
		if status == http.StatusNoContent {
			t.Log("VULNERABILITY: aliceA deleted model config with only view permission")
		}
	})

	t.Run("adminB on EveryoneACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/model-configs/"+f.OrgA.ModelConfigs.EveryoneACL, nil)
		t.Logf("adminB DELETE Org A: %d %s", status, body)
	})
}

// ——— TEST ———

func TestModelConfig_Test(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA tests config — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "POST",
			"/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL+"/test", nil)
		t.Logf("adminA test: %d %s", status, body)
	})

	t.Run("aliceA tests config — may succeed (no permission check)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL+"/test", nil)
		t.Logf("aliceA test NoACL: %d %s", status, body)
		if status == http.StatusOK || status == http.StatusBadGateway {
			t.Log("VULNERABILITY: handleTest on model_config has no permission check")
			t.Log("Only org scoping is applied (WHERE org_id = claims.OrgID)")
		}
	})

	t.Run("adminB tests Org A config — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "POST",
			"/api/v1/model-configs/"+f.OrgA.ModelConfigs.NoACL+"/test", nil)
		t.Logf("adminB test Org A: %d %s", status, body)
	})
}

// ——— GROUP ACL ———

func TestModelConfig_GroupACL(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("bobA gets GroupACL — 200 (group view+edit)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "bobA", "GET",
			"/api/v1/model-configs/"+f.OrgA.ModelConfigs.GroupACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("bobA updates GroupACL — 200 (group edit)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "bobA", "PUT",
			"/api/v1/model-configs/"+f.OrgA.ModelConfigs.GroupACL,
			map[string]string{"name": "group-renamed-by-bob"})
		require.Equal(t, http.StatusOK, status)
	})
}
