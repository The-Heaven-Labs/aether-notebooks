package executor

import (
	"encoding/json"
	"testing"

	"github.com/the-heaven-labs/aether/internal/models"
)

func TestOpenSearchDriver_Type(t *testing.T) {
	d := &OpenSearchDriver{}
	if d.Type() != models.ConnectorOpenSearch {
		t.Fatalf("expected %q, got %q", models.ConnectorOpenSearch, d.Type())
	}
}

func TestOpenSearchDriver_ConfigSchema(t *testing.T) {
	d := &OpenSearchDriver{}
	schema := d.ConfigSchema()
	fieldNames := map[string]bool{}
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}
	for _, expected := range []string{"host", "port", "user", "password", "use_tls"} {
		if !fieldNames[expected] {
			t.Fatalf("expected field %q in config schema", expected)
		}
	}
	for _, unexpected := range []string{"database", "ssl_mode"} {
		if fieldNames[unexpected] {
			t.Fatalf("unexpected field %q in OpenSearch config schema", unexpected)
		}
	}
}

func TestOpenSearchDriver_NewExecutor_ValidConfig(t *testing.T) {
	d := &OpenSearchDriver{}
	cfg := json.RawMessage(`{"host":"localhost","port":9200}`)
	exec, err := d.NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec == nil {
		t.Fatal("expected executor, got nil")
	}
}

func TestOpenSearchDriver_NewExecutor_MissingHost(t *testing.T) {
	d := &OpenSearchDriver{}
	cfg := json.RawMessage(`{"port":9200}`)
	_, err := d.NewExecutor(cfg)
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestOpenSearchDriver_NewExecutor_DefaultPort(t *testing.T) {
	d := &OpenSearchDriver{}
	cfg := json.RawMessage(`{"host":"localhost"}`)
	exec, err := d.NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	osExec, ok := exec.(*OpenSearchExecutor)
	if !ok {
		t.Fatal("expected *OpenSearchExecutor")
	}
	if osExec.baseURL != "http://localhost:9200" {
		t.Fatalf("expected default port 9200, got %q", osExec.baseURL)
	}
}

func TestOpenSearchDriver_NewExecutor_TLS(t *testing.T) {
	d := &OpenSearchDriver{}
	cfg := json.RawMessage(`{"host":"my-opensearch.example.com","use_tls":true}`)
	exec, err := d.NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	osExec, ok := exec.(*OpenSearchExecutor)
	if !ok {
		t.Fatal("expected *OpenSearchExecutor")
	}
	if osExec.baseURL != "https://my-opensearch.example.com:9200" {
		t.Fatalf("expected https URL, got %q", osExec.baseURL)
	}
}
