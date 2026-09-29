// Package dashboard implements dashboard variable validation and
// type-aware interpolation into query widget SQL. Interpolation is
// escaping-only: values can never become identifiers or SQL fragments.
package dashboard

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/the-heaven-labs/aether/internal/models"
)

var (
	refRe  = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}`)
	nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	types  = map[string]bool{
		"text": true, "number": true, "boolean": true, "date": true,
		"date_range": true, "single_select": true, "multi_select": true,
	}
)

func ValidateVariables(vars []models.DashboardVariable) error {
	seen := map[string]bool{}
	for _, v := range vars {
		if !nameRe.MatchString(v.Name) {
			return fmt.Errorf("invalid variable name %q", v.Name)
		}
		if seen[v.Name] {
			return fmt.Errorf("duplicate variable name %q", v.Name)
		}
		if !types[v.Type] {
			return fmt.Errorf("invalid type %q for variable %q", v.Type, v.Name)
		}
		if v.Options != nil {
			switch v.Options.Mode {
			case "static":
			case "query":
				if v.Options.Query == nil || v.Options.Query.ConnectorID == "" || v.Options.Query.SQL == "" {
					return fmt.Errorf("variable %q has an incomplete options query", v.Name)
				}
			default:
				return fmt.Errorf("variable %q has invalid options mode %q", v.Name, v.Options.Mode)
			}
		}
		seen[v.Name] = true
	}
	for _, v := range vars {
		for _, dep := range v.DependsOn {
			if !seen[dep] {
				return fmt.Errorf("variable %q depends on unknown variable %q", v.Name, dep)
			}
		}
	}
	return nil
}

// Interpolate substitutes every {{token}} in sql with a SQL literal derived
// from the matching variable's declared type. provided holds raw JSON values
// from the client; missing entries fall back to the variable default.
func Interpolate(sql string, vars []models.DashboardVariable, provided map[string]any) (string, error) {
	literals, err := resolveLiterals(vars, provided)
	if err != nil {
		return "", err
	}
	var firstErr error
	out := refRe.ReplaceAllStringFunc(sql, func(token string) string {
		name := refRe.FindStringSubmatch(token)[1]
		lit, ok := literals[name]
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("unknown variable %q referenced in query", name)
			}
			return token
		}
		return lit
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

func resolveLiterals(vars []models.DashboardVariable, provided map[string]any) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range vars {
		raw, ok := provided[v.Name]
		if !ok || raw == nil {
			raw = v.Default
		}
		if v.Type == "date_range" {
			start, end, err := asStringPair(raw, v)
			if err != nil {
				return nil, err
			}
			out[v.Name+"_start"] = quote(start)
			out[v.Name+"_end"] = quote(end)
			continue
		}
		lit, err := formatValue(v, raw)
		if err != nil {
			return nil, err
		}
		out[v.Name] = lit
	}
	return out, nil
}

func formatValue(v models.DashboardVariable, raw any) (string, error) {
	empty := raw == nil || raw == ""
	switch v.Type {
	case "text", "single_select", "":
		s, err := asString(raw, v)
		if err != nil {
			return "", err
		}
		if v.Required && s == "" {
			return "", fmt.Errorf("variable %q is required", v.Name)
		}
		return quote(s), nil
	case "number":
		if empty {
			if v.Required {
				return "", fmt.Errorf("variable %q is required", v.Name)
			}
			return "NULL", nil
		}
		num, err := asNumber(raw, v)
		if err != nil {
			return "", err
		}
		return strconv.FormatFloat(num, 'f', -1, 64), nil
	case "boolean":
		if empty {
			if v.Required {
				return "", fmt.Errorf("variable %q is required", v.Name)
			}
			return "NULL", nil
		}
		b, ok := raw.(bool)
		if !ok {
			return "", fmt.Errorf("invalid value for variable %q: expected a boolean", v.Name)
		}
		return strconv.FormatBool(b), nil
	case "date":
		s, err := asString(raw, v)
		if err != nil {
			return "", err
		}
		if v.Required && s == "" {
			return "", fmt.Errorf("variable %q is required", v.Name)
		}
		if s != "" {
			if _, err := parseDate(s); err != nil {
				return "", fmt.Errorf("invalid value for variable %q: %v", v.Name, err)
			}
		}
		return quote(s), nil
	case "multi_select":
		list, err := asStringList(raw, v)
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			if v.Required {
				return "", fmt.Errorf("variable %q is required", v.Name)
			}
			return "(NULL)", nil
		}
		parts := make([]string, 0, len(list))
		for _, item := range list {
			parts = append(parts, quote(item))
		}
		return "(" + strings.Join(parts, ",") + ")", nil
	default:
		return "", fmt.Errorf("unsupported variable type %q", v.Type)
	}
}

func asString(raw any, v models.DashboardVariable) (string, error) {
	if raw == nil {
		return "", nil
	}
	if s, ok := raw.(string); ok {
		return s, nil
	}
	return "", fmt.Errorf("invalid value for variable %q: expected a string", v.Name)
}

func asNumber(raw any, v models.DashboardVariable) (float64, error) {
	switch n := raw.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, fmt.Errorf("invalid value for variable %q", v.Name)
		}
		return n, nil
	case int:
		return float64(n), nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, fmt.Errorf("invalid value for variable %q: not a number", v.Name)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("invalid value for variable %q: not a number", v.Name)
	}
}

func asStringList(raw any, v models.DashboardVariable) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		if ss, ok := raw.([]string); ok {
			return ss, nil
		}
		return nil, fmt.Errorf("invalid value for variable %q: expected a list", v.Name)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("invalid value for variable %q: list items must be strings", v.Name)
		}
		out = append(out, s)
	}
	return out, nil
}

func asStringPair(raw any, v models.DashboardVariable) (string, string, error) {
	empty := raw == nil || raw == ""
	if empty {
		if v.Required {
			return "", "", fmt.Errorf("variable %q is required", v.Name)
		}
		return "", "", nil
	}
	list, ok := raw.([]any)
	if !ok || len(list) != 2 {
		if ss, ok := raw.([]string); ok && len(ss) == 2 {
			return validatePair(ss[0], ss[1], v)
		}
		return "", "", fmt.Errorf("invalid value for variable %q: expected a [start, end] pair", v.Name)
	}
	start, ok1 := list[0].(string)
	end, ok2 := list[1].(string)
	if !ok1 || !ok2 {
		return "", "", fmt.Errorf("invalid value for variable %q: expected a [start, end] pair", v.Name)
	}
	return validatePair(start, end, v)
}

func validatePair(start, end string, v models.DashboardVariable) (string, string, error) {
	for _, s := range []string{start, end} {
		if s == "" {
			continue
		}
		if _, err := parseDate(s); err != nil {
			return "", "", fmt.Errorf("invalid value for variable %q: %v", v.Name, err)
		}
	}
	return start, end, nil
}

func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
