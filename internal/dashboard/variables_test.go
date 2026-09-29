package dashboard

import (
	"strings"
	"testing"

	"github.com/the-heaven-labs/aether/internal/models"
)

func varOf(name, typ string, def interface{}) models.DashboardVariable {
	return models.DashboardVariable{Name: name, Type: typ, Default: def}
}

func TestInterpolateEscapesText(t *testing.T) {
	sql, err := Interpolate(
		`SELECT * FROM t WHERE name = {{who}}`,
		[]models.DashboardVariable{varOf("who", "text", "")},
		map[string]any{"who": "O'Brien"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, `'O''Brien'`) {
		t.Fatalf("not escaped: %s", sql)
	}
}

func TestInterpolateAllowsSpacesInsideToken(t *testing.T) {
	sql, err := Interpolate(`SELECT {{ x }}`, []models.DashboardVariable{varOf("x", "number", 1)}, map[string]any{"x": 42.5})
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT 42.5" {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateRejectsUnknownVariable(t *testing.T) {
	_, err := Interpolate(`SELECT {{nope}}`, nil, nil)
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("expected unknown variable error, got %v", err)
	}
}

func TestInterpolateMultiSelect(t *testing.T) {
	sql, err := Interpolate(`WHERE region IN {{region}}`,
		[]models.DashboardVariable{varOf("region", "multi_select", nil)},
		map[string]any{"region": []any{"EMEA", "AMER"}})
	if err != nil {
		t.Fatal(err)
	}
	if sql != "WHERE region IN ('EMEA','AMER')" {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateMultiSelectEmptyOptionalIsNull(t *testing.T) {
	sql, err := Interpolate(`WHERE region IN {{region}}`,
		[]models.DashboardVariable{varOf("region", "multi_select", nil)},
		map[string]any{"region": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if sql != "WHERE region IN (NULL)" {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateRequiredEmptyErrors(t *testing.T) {
	v := models.DashboardVariable{Name: "region", Type: "multi_select", Required: true}
	if _, err := Interpolate(`WHERE region IN {{region}}`, []models.DashboardVariable{v}, map[string]any{"region": []any{}}); err == nil {
		t.Fatal("expected required error")
	}
}

func TestInterpolateNumberRejectsNonNumeric(t *testing.T) {
	if _, err := Interpolate(`SELECT {{n}}`, []models.DashboardVariable{varOf("n", "number", 0)}, map[string]any{"n": "abc"}); err == nil {
		t.Fatal("expected number error")
	}
}

func TestInterpolateBoolean(t *testing.T) {
	sql, err := Interpolate(`SELECT * FROM t WHERE active = {{a}}`,
		[]models.DashboardVariable{varOf("a", "boolean", false)}, map[string]any{"a": true})
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT * FROM t WHERE active = true" {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateDateRange(t *testing.T) {
	v := models.DashboardVariable{Name: "range", Type: "date_range"}
	sql, err := Interpolate(`WHERE ts >= {{range_start}} AND ts < {{range_end}}`,
		[]models.DashboardVariable{v}, map[string]any{"range": []any{"2026-01-01", "2026-02-01"}})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `WHERE ts >= '2026-01-01' AND ts < '2026-02-01'` {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateDateRejectsGarbage(t *testing.T) {
	if _, err := Interpolate(`SELECT {{d}}`, []models.DashboardVariable{varOf("d", "date", "")}, map[string]any{"d": "not-a-date"}); err == nil {
		t.Fatal("expected date error")
	}
}

func TestInterpolateDefaultUsedWhenValueMissing(t *testing.T) {
	sql, err := Interpolate(`SELECT {{n}}`, []models.DashboardVariable{varOf("n", "number", 7)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT 7" {
		t.Fatalf("got %q", sql)
	}
}

func TestValidateVariablesRejectsBadInput(t *testing.T) {
	if err := ValidateVariables([]models.DashboardVariable{{Name: "a b", Type: "text"}}); err == nil {
		t.Fatal("expected invalid name error")
	}
	if err := ValidateVariables([]models.DashboardVariable{{Name: "a", Type: "wat"}}); err == nil {
		t.Fatal("expected invalid type error")
	}
	if err := ValidateVariables([]models.DashboardVariable{
		{Name: "a", Type: "multi_select", Options: &models.VariableOptions{Mode: "static"}},
		{Name: "b", Type: "text", DependsOn: []string{"missing"}},
	}); err == nil {
		t.Fatal("expected unknown dependency error")
	}
}
