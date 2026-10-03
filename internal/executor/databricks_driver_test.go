package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/the-heaven-labs/aether/internal/models"
)

func TestDatabricksDriver_Type(t *testing.T) {
	d := &DatabricksDriver{}
	if d.Type() != models.ConnectorDatabricks {
		t.Fatalf("expected %q, got %q", models.ConnectorDatabricks, d.Type())
	}
}

func TestDatabricksDriver_ConfigSchemaSecrets(t *testing.T) {
	d := &DatabricksDriver{}
	secrets := map[string]bool{}
	for _, f := range d.ConfigSchema().Fields {
		if f.Secret {
			secrets[f.Name] = true
		}
	}
	if !secrets["token"] || !secrets["client_secret"] {
		t.Fatalf("token and client_secret must be secret, got %v", secrets)
	}
}

func TestValidateDatabricksConfig(t *testing.T) {
	cases := []struct {
		name    string
		in      databricksConfig
		wantErr string
	}{
		{"missing host", databricksConfig{HTTPPath: "/sql/1.0/warehouses/x", Token: "t"}, "host"},
		{"missing http_path", databricksConfig{Host: "h", Token: "t"}, "http_path"},
		{"pat without token", databricksConfig{Host: "h", HTTPPath: "/p"}, "token"},
		{"m2m without credentials", databricksConfig{Host: "h", HTTPPath: "/p", AuthType: "oauth_m2m"}, "client_id"},
		{"unknown auth", databricksConfig{Host: "h", HTTPPath: "/p", AuthType: "bogus"}, "auth_type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateDatabricksConfig(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateDatabricksConfigDefaultsAndNormalization(t *testing.T) {
	got, err := validateDatabricksConfig(databricksConfig{
		Host: "https://dbc-abc.cloud.databricks.com/", HTTPPath: "/sql/1.0/warehouses/x",
		Token: "t",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Host != "dbc-abc.cloud.databricks.com" {
		t.Fatalf("host not normalized: %q", got.Host)
	}
	if got.AuthType != "pat" || got.Port != 443 {
		t.Fatalf("expected auth_type=pat port=443 defaults, got %q/%d", got.AuthType, got.Port)
	}
}

func TestDatabricksDriver_NewExecutor_InvalidConfig(t *testing.T) {
	d := &DatabricksDriver{}
	_, err := d.NewExecutor(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}
