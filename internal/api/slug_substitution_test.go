package api

import (
	"strings"
	"testing"
)

func TestResolveSlugRefs_NoRefs(t *testing.T) {
	slugMap := map[string]string{"cell_a": "SELECT 1"}
	result, err := resolveSlugRefs("SELECT * FROM foo", slugMap, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "SELECT * FROM foo" {
		t.Fatalf("expected no change, got %q", result)
	}
}

func TestResolveSlugRefs_SimpleSubstitution(t *testing.T) {
	slugMap := map[string]string{"cell_a": "SELECT id FROM users"}
	result, err := resolveSlugRefs("SELECT * FROM ({{cell_a}}) t", slugMap, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "SELECT * FROM ((SELECT id FROM users)) t" {
		t.Fatalf("got %q", result)
	}
}

func TestResolveSlugRefs_UnknownSlug(t *testing.T) {
	_, err := resolveSlugRefs("SELECT * FROM ({{missing_cell}}) t", map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected error for unknown slug")
	}
}

func TestResolveSlugRefs_DirectCycle(t *testing.T) {
	slugMap := map[string]string{
		"cell_a": "SELECT * FROM ({{cell_a}}) t",
	}
	_, err := resolveSlugRefs("SELECT * FROM ({{cell_a}}) t", slugMap, nil)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestResolveSlugRefs_IndirectCycle(t *testing.T) {
	slugMap := map[string]string{
		"cell_a": "SELECT * FROM ({{cell_b}}) t",
		"cell_b": "SELECT * FROM ({{cell_a}}) t",
	}
	_, err := resolveSlugRefs("SELECT * FROM ({{cell_a}}) t", slugMap, nil)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestResolveSlugRefs_NestedResolution(t *testing.T) {
	slugMap := map[string]string{
		"cell_a": "SELECT id FROM users",
		"cell_b": "SELECT * FROM ({{cell_a}}) t WHERE id > 5",
	}
	result, err := resolveSlugRefs("SELECT * FROM ({{cell_b}}) outer", slugMap, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := "SELECT * FROM ((SELECT * FROM ((SELECT id FROM users)) t WHERE id > 5)) outer"
	if result != expected {
		t.Fatalf("got %q\nwant %q", result, expected)
	}
}
