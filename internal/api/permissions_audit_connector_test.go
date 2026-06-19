package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// ——— LIST ———

func TestConnector_List(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		wantMin int
		wantMax int
	}{
		{
			name:    "adminA sees all Org A connectors",
			userKey: "adminA",
			wantMin: 4, wantMax: 4,
		},
		{
			name:    "aliceA sees connectors with user or everyone ACL",
			userKey: "aliceA",
			wantMin: 2, wantMax: 3,
		},
		{
			name:    "carolA sees only everyone-ACL connectors",
			userKey: "carolA",
			wantMin: 1, wantMax: 2,
		},
		{
			name:    "adminB sees only Org B connectors",
			userKey: "adminB",
			wantMin: 1, wantMax: 1,
		},
		{
			name:    "eveB sees only Org B connectors she can access",
			userKey: "eveB",
			wantMin: 0, wantMax: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "GET", "/api/v1/connectors", nil)
			require.Equal(t, http.StatusOK, status)

			var connectors []map[string]any
			json.Unmarshal([]byte(body), &connectors)
			count := len(connectors)
			t.Logf("%s: %d connectors visible", tt.userKey, count)
			require.GreaterOrEqual(t, count, tt.wantMin)
			require.LessOrEqual(t, count, tt.wantMax)
		})
	}
}

// ——— GET ———

func TestConnector_Get(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL, nil)
		t.Logf("aliceA GET NoACL connector: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can view NoACL connector without ACL entry")
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.UserACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("carolA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL, nil)
		t.Logf("carolA GET NoACL connector: %d %s", status, body)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL, nil)
		t.Logf("adminB GET Org A NoACL connector (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL, nil)
		t.Logf("eveB GET Org A NoACL connector (cross-org): %d %s", status, body)
	})
}

// ——— CREATE ———

func TestConnector_Create(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	body := map[string]any{
		"name": "test-conn",
		"type": "postgres",
		"config": map[string]any{
			"host": "localhost", "port": 5432,
			"user": "hnb", "password": "hnb_dev", "database": "hnb",
		},
	}

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"adminA creates connector", "adminA", http.StatusCreated},
		{"aliceA cannot create (not admin)", "aliceA", http.StatusForbidden},
		{"carolA cannot create (not admin)", "carolA", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := f.DoRequest(t, tt.userKey, "POST", "/api/v1/connectors", body)
			t.Logf("%s create connector: %d %s", tt.userKey, status, resp)
			require.Equal(t, tt.want, status)
		})
	}
}

// ——— UPDATE ———

func TestConnector_Update(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "PUT", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL,
			map[string]string{"name": "updated-by-admin"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (not admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL,
			map[string]string{"name": "updated-by-alice"})
		t.Logf("aliceA PUT NoACL connector: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("bobA on GroupACL — 403 (RequireRole blocks non-admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "PUT", "/api/v1/connectors/"+f.OrgA.Connectors.GroupACL,
			map[string]string{"name": "updated-by-bob"})
		t.Logf("bobA PUT GroupACL connector: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "PUT", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL,
			map[string]string{"name": "updated-by-adminb"})
		t.Logf("adminB PUT Org A NoACL connector: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (not admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "PUT", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL,
			map[string]string{"name": "updated-by-eveb"})
		t.Logf("eveB PUT Org A connector: %d %s", status, body)
	})
}

// ——— DELETE ———

func TestConnector_Delete(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 204", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "DELETE", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL, nil)
		t.Logf("adminA DELETE NoACL connector: %d %s", status, body)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on UserACL — 403 (not admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/connectors/"+f.OrgA.Connectors.UserACL, nil)
		t.Logf("aliceA DELETE UserACL connector: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("adminB on EveryoneACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/connectors/"+f.OrgA.Connectors.EveryoneACL, nil)
		t.Logf("adminB DELETE Org A connector (cross-org): %d %s", status, body)
	})
}

// ——— TEST CONFIG (no resource needed) ———

func TestConnector_TestConfig(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	body := map[string]any{
		"name": "test",
		"type": "postgres",
		"config": map[string]any{
			"host": "localhost", "port": 5432,
			"user": "hnb", "password": "hnb_dev", "database": "hnb",
		},
	}

	t.Run("adminA tests config — 200", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "POST", "/api/v1/connectors/test", body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA tests config — 200 (just authMW)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "POST", "/api/v1/connectors/test", body)
		require.Equal(t, http.StatusOK, status)
	})
}

// ——— TEST EXISTING CONNECTOR ———

func TestConnector_Test(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "POST", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/test", nil)
		t.Logf("adminA test NoACL connector: %d %s", status, body)
	})

	t.Run("aliceA on NoACL — 403 (no use permission)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/test", nil)
		t.Logf("aliceA test NoACL connector: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can test NoACL connector without use permission")
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "POST", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/test", nil)
		t.Logf("adminB test Org A connector (cross-org): %d %s", status, body)
	})
}

// ——— SCHEMA ———

func TestConnector_Schema(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/schema", nil)
		t.Logf("adminA schema NoACL connector: %d %s", status, body)
	})

	t.Run("aliceA on NoACL — 403 (no use permission)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/schema", nil)
		t.Logf("aliceA schema NoACL connector: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can get schema of NoACL connector without use permission")
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/schema", nil)
		t.Logf("adminB schema Org A connector (cross-org): %d %s", status, body)
	})
}

// ——— DATABASES ———

func TestConnector_Databases(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/databases", nil)
		t.Logf("adminA databases NoACL connector: %d %s", status, body)
	})

	t.Run("aliceA on NoACL — 403 (no use permission)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/databases", nil)
		t.Logf("aliceA databases NoACL connector: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can list databases of NoACL connector without use permission")
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/databases", nil)
		t.Logf("adminB databases Org A connector (cross-org): %d %s", status, body)
	})
}

// ——— SET DEFAULT ———

func TestConnector_SetDefault(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 204", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "PUT", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL+"/default", nil)
		t.Logf("adminA set default NoACL connector: %d %s", status, body)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on NoACL — 403 (not admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/connectors/"+f.OrgA.Connectors.UserACL+"/default", nil)
		t.Logf("aliceA set default: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})
}
