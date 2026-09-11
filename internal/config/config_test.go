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
