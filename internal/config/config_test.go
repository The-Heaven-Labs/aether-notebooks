package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := LoadMigrateOnly()
	if err != nil {
		t.Fatalf("LoadMigrateOnly() failed: %v", err)
	}
	if cfg.APIURL != "" {
		t.Errorf("expected empty APIURL, got %q", cfg.APIURL)
	}
	if cfg.RelayURL != "" {
		t.Errorf("expected empty RelayURL, got %q", cfg.RelayURL)
	}
}

func TestLoadCustomAPIURL(t *testing.T) {
	os.Setenv("AETHER_API_URL", "https://api.example.com")
	defer os.Unsetenv("AETHER_API_URL")

	cfg, err := LoadMigrateOnly()
	if err != nil {
		t.Fatalf("LoadMigrateOnly() failed: %v", err)
	}
	if cfg.APIURL != "https://api.example.com" {
		t.Errorf("expected APIURL=https://api.example.com, got %q", cfg.APIURL)
	}
}

func TestLoadCustomRelayURL(t *testing.T) {
	os.Setenv("AETHER_RELAY_URL", "wss://relay.example.com")
	defer os.Unsetenv("AETHER_RELAY_URL")

	cfg, err := LoadMigrateOnly()
	if err != nil {
		t.Fatalf("LoadMigrateOnly() failed: %v", err)
	}
	if cfg.RelayURL != "wss://relay.example.com" {
		t.Errorf("expected RelayURL=wss://relay.example.com, got %q", cfg.RelayURL)
	}
}

func TestLoadBothURLs(t *testing.T) {
	os.Setenv("AETHER_API_URL", "https://api.example.com")
	os.Setenv("AETHER_RELAY_URL", "wss://relay.example.com")
	defer os.Unsetenv("AETHER_API_URL")
	defer os.Unsetenv("AETHER_RELAY_URL")

	cfg, err := LoadMigrateOnly()
	if err != nil {
		t.Fatalf("LoadMigrateOnly() failed: %v", err)
	}
	if cfg.APIURL != "https://api.example.com" {
		t.Errorf("expected APIURL=https://api.example.com, got %q", cfg.APIURL)
	}
	if cfg.RelayURL != "wss://relay.example.com" {
		t.Errorf("expected RelayURL=wss://relay.example.com, got %q", cfg.RelayURL)
	}
}

func TestStatsRollupIntervalDefault(t *testing.T) {
	os.Unsetenv("AETHER_AGENT_STATS_ROLLUP_INTERVAL")
	cfg, err := LoadMigrateOnly()
	if err != nil {
		t.Fatalf("LoadMigrateOnly() failed: %v", err)
	}
	if cfg.StatsRollupInterval.String() != "1h0m0s" {
		t.Errorf("expected default 1h, got %v", cfg.StatsRollupInterval)
	}
}

func TestStatsRollupIntervalCustomAndFloor(t *testing.T) {
	for raw, want := range map[string]string{
		"30m": "30m0s",
		"2h":  "2h0m0s",
		"1m":  "5m0s", // below floor → raised
		"30s": "5m0s",
	} {
		os.Setenv("AETHER_AGENT_STATS_ROLLUP_INTERVAL", raw)
		cfg, err := LoadMigrateOnly()
		if err != nil {
			t.Fatalf("LoadMigrateOnly() failed for %q: %v", raw, err)
		}
		if cfg.StatsRollupInterval.String() != want {
			t.Errorf("for %q: expected %s, got %v", raw, want, cfg.StatsRollupInterval)
		}
		os.Unsetenv("AETHER_AGENT_STATS_ROLLUP_INTERVAL")
	}
}

func TestStatsRollupIntervalInvalid(t *testing.T) {
	os.Setenv("AETHER_AGENT_STATS_ROLLUP_INTERVAL", "not-a-duration")
	defer os.Unsetenv("AETHER_AGENT_STATS_ROLLUP_INTERVAL")
	if _, err := LoadMigrateOnly(); err == nil {
		t.Error("expected error for invalid duration, got nil")
	}
}

func TestAgentToolTimeoutDefault(t *testing.T) {
	os.Unsetenv("AETHER_AGENT_TOOL_TIMEOUT_DEFAULT")
	cfg, err := LoadMigrateOnly()
	if err != nil {
		t.Fatalf("LoadMigrateOnly() failed: %v", err)
	}
	if cfg.AgentToolTimeoutDefault.String() != "2m0s" {
		t.Errorf("expected default 2m, got %v", cfg.AgentToolTimeoutDefault)
	}
}

func TestAgentToolTimeoutCustomAndFloor(t *testing.T) {
	for raw, want := range map[string]string{
		"45s":   "45s",
		"5m":    "5m0s",
		"500ms": "1s", // below floor → raised
		"-3s":   "1s",
	} {
		os.Setenv("AETHER_AGENT_TOOL_TIMEOUT_DEFAULT", raw)
		cfg, err := LoadMigrateOnly()
		if err != nil {
			t.Fatalf("LoadMigrateOnly() failed for %q: %v", raw, err)
		}
		if cfg.AgentToolTimeoutDefault.String() != want {
			t.Errorf("for %q: expected %s, got %v", raw, want, cfg.AgentToolTimeoutDefault)
		}
		os.Unsetenv("AETHER_AGENT_TOOL_TIMEOUT_DEFAULT")
	}
}

func TestAgentToolTimeoutInvalid(t *testing.T) {
	os.Setenv("AETHER_AGENT_TOOL_TIMEOUT_DEFAULT", "not-a-duration")
	defer os.Unsetenv("AETHER_AGENT_TOOL_TIMEOUT_DEFAULT")
	if _, err := LoadMigrateOnly(); err == nil {
		t.Error("expected error for invalid duration, got nil")
	}
}

func TestWarehouseReconcileIntervalDefault(t *testing.T) {
	os.Unsetenv("AETHER_CH_RECONCILE_INTERVAL")
	cfg, err := LoadMigrateOnly()
	if err != nil {
		t.Fatalf("LoadMigrateOnly() failed: %v", err)
	}
	if cfg.WarehouseReconcileInterval.String() != "10m0s" {
		t.Errorf("expected default 10m, got %v", cfg.WarehouseReconcileInterval)
	}
}

func TestWarehouseReconcileIntervalCustomAndFloor(t *testing.T) {
	for raw, want := range map[string]string{
		"30m": "30m0s",
		"2h":  "2h0m0s",
		"1m":  "1m0s",
		"30s": "1m0s", // below floor → raised
		"-5m": "1m0s",
	} {
		os.Setenv("AETHER_CH_RECONCILE_INTERVAL", raw)
		cfg, err := LoadMigrateOnly()
		if err != nil {
			t.Fatalf("LoadMigrateOnly() failed for %q: %v", raw, err)
		}
		if cfg.WarehouseReconcileInterval.String() != want {
			t.Errorf("for %q: expected %s, got %v", raw, want, cfg.WarehouseReconcileInterval)
		}
		os.Unsetenv("AETHER_CH_RECONCILE_INTERVAL")
	}
}

func TestWarehouseReconcileIntervalInvalid(t *testing.T) {
	os.Setenv("AETHER_CH_RECONCILE_INTERVAL", "not-a-duration")
	defer os.Unsetenv("AETHER_CH_RECONCILE_INTERVAL")
	if _, err := LoadMigrateOnly(); err == nil {
		t.Error("expected error for invalid duration, got nil")
	}
}

func TestOutputLimitsMaxBytesDefault(t *testing.T) {
	os.Unsetenv("AETHER_OUTPUT_LIMITS_MAX_BYTES")
	cfg, err := LoadMigrateOnly()
	if err != nil {
		t.Fatalf("LoadMigrateOnly() failed: %v", err)
	}
	if cfg.OutputLimitsMaxBytes != 64*1024*1024 {
		t.Errorf("expected default 64MB ceiling, got %d", cfg.OutputLimitsMaxBytes)
	}
}

func TestOutputLimitsMaxBytesCustom(t *testing.T) {
	os.Setenv("AETHER_OUTPUT_LIMITS_MAX_BYTES", "1048576")
	defer os.Unsetenv("AETHER_OUTPUT_LIMITS_MAX_BYTES")
	cfg, err := LoadMigrateOnly()
	if err != nil {
		t.Fatalf("LoadMigrateOnly() failed: %v", err)
	}
	if cfg.OutputLimitsMaxBytes != 1048576 {
		t.Errorf("expected 1MB ceiling, got %d", cfg.OutputLimitsMaxBytes)
	}
}

func TestOutputLimitsMaxBytesInvalid(t *testing.T) {
	os.Setenv("AETHER_OUTPUT_LIMITS_MAX_BYTES", "not-a-number")
	defer os.Unsetenv("AETHER_OUTPUT_LIMITS_MAX_BYTES")
	if _, err := LoadMigrateOnly(); err == nil {
		t.Error("expected error for invalid ceiling, got nil")
	}
}

func TestResolveOutputLimit(t *testing.T) {
	tests := []struct {
		name        string
		orgValue    int64
		platformMax int64
		want        int64
	}{
		{"org default clamped by platform ceiling", 10 * 1024 * 1024, 64 * 1024 * 1024, 10 * 1024 * 1024},
		{"org above ceiling clamped down", 128 * 1024 * 1024, 64 * 1024 * 1024, 64 * 1024 * 1024},
		{"org zero is unlimited", 0, 64 * 1024 * 1024, 0},
		{"org negative is unlimited", -1, 64 * 1024 * 1024, 0},
		{"no ceiling configured passes org value", 10 * 1024 * 1024, 0, 10 * 1024 * 1024},
		{"no ceiling and unlimited org stays unlimited", 0, 0, 0},
		{"org equal to ceiling unchanged", 64 * 1024 * 1024, 64 * 1024 * 1024, 64 * 1024 * 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveOutputLimit(tt.orgValue, tt.platformMax); got != tt.want {
				t.Errorf("ResolveOutputLimit(%d, %d) = %d, want %d", tt.orgValue, tt.platformMax, got, tt.want)
			}
		})
	}
}
