package auth

import (
	"reflect"
	"testing"
)

func TestRedactTokenMaterial(t *testing.T) {
	claims := map[string]any{
		"email":         "alice@example.com",
		"groups":        []any{"aether-analysts", "all-employees"},
		"id_token":      "eyJ...",
		"accessToken":   "camel-case-token",
		"refresh-token": "hyphen-token",
		"client_secret": "shh",
		"password":      "hunter2",
		"authorization": "Bearer abc",
		"nested": map[string]any{
			"keep":          "value",
			"access_token":  "nested-token",
			"client-secret": "nested-secret",
		},
		"list": []any{
			map[string]any{"refresh_token": "in-list", "keep": true},
			"plain",
		},
	}

	redacted := RedactTokenMaterial(claims)

	kept := map[string]any{
		"email":  "alice@example.com",
		"groups": []any{"aether-analysts", "all-employees"},
		"nested": map[string]any{"keep": "value"},
		"list": []any{
			map[string]any{"keep": true},
			"plain",
		},
	}
	if !reflect.DeepEqual(redacted, kept) {
		t.Fatalf("unexpected redaction result:\n got: %#v\nwant: %#v", redacted, kept)
	}

	// The input map must not be mutated.
	if _, ok := claims["access_token"]; ok {
		t.Fatalf("redaction mutated the input claims map")
	}
}

func TestRedactTokenMaterialNil(t *testing.T) {
	if got := RedactTokenMaterial(nil); got != nil {
		t.Fatalf("expected nil for nil input, got %#v", got)
	}
}

func TestParseGroupsClaimShapes(t *testing.T) {
	tests := []struct {
		name       string
		raw        any
		wantGroups []string
		wantOK     bool
	}{
		{"missing", nil, nil, false},
		{"array of strings", []any{"a", "b"}, []string{"a", "b"}, true},
		{"array with non-strings", []any{"a", 7, true}, []string{"a"}, true},
		{"empty array", []any{}, nil, true},
		{"comma string", "a,b", nil, false},
		{"object", map[string]any{"a": true}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups, ok := parseGroupsClaim(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !reflect.DeepEqual(groups, tt.wantGroups) {
				t.Fatalf("groups = %#v, want %#v", groups, tt.wantGroups)
			}
		})
	}
}
