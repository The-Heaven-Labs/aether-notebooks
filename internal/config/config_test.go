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
