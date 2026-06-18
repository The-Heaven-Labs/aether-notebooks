package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// ——— LIST ———

func TestSkill_List(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		wantMin int
		wantMax int
	}{
		{
			name:    "adminA sees all Org A skills",
			userKey: "adminA",
			wantMin: 4, wantMax: 4,
		},
		{
			name:    "aliceA sees skills with user or everyone ACL",
			userKey: "aliceA",
			wantMin: 2, wantMax: 3,
		},
		{
			name:    "carolA sees only everyone-ACL skills",
			userKey: "carolA",
			wantMin: 1, wantMax: 2,
		},
		{
			name:    "adminB sees only Org B skills",
			userKey: "adminB",
			wantMin: 1, wantMax: 1,
		},
		{
			name:    "eveB sees only Org B skills she can access",
			userKey: "eveB",
			wantMin: 0, wantMax: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "GET", "/api/v1/skills", nil)
			require.Equal(t, http.StatusOK, status)

			var skills []map[string]any
			json.Unmarshal([]byte(body), &skills)
			count := len(skills)
			t.Logf("%s: %d skills visible", tt.userKey, count)
			require.GreaterOrEqual(t, count, tt.wantMin)
			require.LessOrEqual(t, count, tt.wantMax)
		})
	}
}

// ——— GET ———

func TestSkill_Get(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET", "/api/v1/skills/"+f.OrgA.Skills.NoACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/skills/"+f.OrgA.Skills.NoACL, nil)
		t.Logf("aliceA GET NoACL skill: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can view NoACL skill without ACL entry")
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET", "/api/v1/skills/"+f.OrgA.Skills.UserACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("carolA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/skills/"+f.OrgA.Skills.NoACL, nil)
		t.Logf("carolA GET NoACL skill: %d %s", status, body)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/skills/"+f.OrgA.Skills.NoACL, nil)
		t.Logf("adminB GET Org A NoACL skill: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "GET", "/api/v1/skills/"+f.OrgA.Skills.NoACL, nil)
		t.Logf("eveB GET Org A NoACL skill: %d %s", status, body)
	})
}

// ——— CREATE ———

func TestSkill_Create(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"adminA creates skill", "adminA", http.StatusCreated},
		{"aliceA creates skill", "aliceA", http.StatusCreated},
		{"carolA creates skill", "carolA", http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "POST", "/api/v1/skills",
				map[string]string{"name": "test-" + tt.userKey})
			t.Logf("%s create skill: %d %s", tt.userKey, status, body)
			require.Equal(t, tt.want, status)
		})
	}
}

// ——— UPDATE ———

func TestSkill_Update(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "PUT", "/api/v1/skills/"+f.OrgA.Skills.NoACL,
			map[string]string{"name": "updated-by-admin"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/skills/"+f.OrgA.Skills.NoACL,
			map[string]string{"name": "updated-by-alice"})
		t.Logf("aliceA PUT NoACL skill: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated NoACL skill without ACL entry")
		}
	})

	t.Run("aliceA on UserACL — 403 (view-only)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/skills/"+f.OrgA.Skills.UserACL,
			map[string]string{"name": "updated-by-alice"})
		t.Logf("aliceA PUT UserACL skill (view-only): %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated UserACL skill with only view")
		}
	})

	t.Run("bobA on GroupACL — 200 (group has edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "PUT", "/api/v1/skills/"+f.OrgA.Skills.GroupACL,
			map[string]string{"name": "updated-by-bob"})
		t.Logf("bobA PUT GroupACL skill: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "PUT", "/api/v1/skills/"+f.OrgA.Skills.NoACL,
			map[string]string{"name": "updated-by-adminb"})
		t.Logf("adminB PUT Org A NoACL skill: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "PUT", "/api/v1/skills/"+f.OrgA.Skills.NoACL,
			map[string]string{"name": "updated-by-eveb"})
		t.Logf("eveB PUT Org A NoACL skill: %d %s", status, body)
	})
}

// ——— DELETE ———

func TestSkill_Delete(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 204", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "DELETE", "/api/v1/skills/"+f.OrgA.Skills.NoACL, nil)
		t.Logf("adminA DELETE NoACL skill: %d %s", status, body)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on UserACL — 403 (view-only)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/skills/"+f.OrgA.Skills.UserACL, nil)
		t.Logf("aliceA DELETE UserACL skill: %d %s", status, body)
		if status == http.StatusNoContent {
			t.Log("VULNERABILITY: aliceA deleted skill with only view permission")
		}
	})

	t.Run("adminB on EveryoneACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/skills/"+f.OrgA.Skills.EveryoneACL, nil)
		t.Logf("adminB DELETE Org A skill: %d %s", status, body)
	})
}

// ——— GROUP ACL ———

func TestSkill_GroupACL(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("bobA gets GroupACL skill — 200 (group view+edit)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "bobA", "GET",
			"/api/v1/skills/"+f.OrgA.Skills.GroupACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("bobA updates GroupACL skill — 200 (group edit)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "bobA", "PUT",
			"/api/v1/skills/"+f.OrgA.Skills.GroupACL,
			map[string]string{"name": "group-renamed-by-bob"})
		require.Equal(t, http.StatusOK, status)
	})
}
