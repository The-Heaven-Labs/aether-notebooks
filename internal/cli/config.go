package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type Config struct {
	APIURL string `json:"api_url"`
}

var configKeys = map[string]struct {
	Field *string
	Desc  string
}{
	"api-url": {Desc: "Default API server URL (e.g. http://localhost:8088)"},
}

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aether")
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

func knownConfigKeys() []string {
	keys := make([]string, 0, len(configKeys))
	for k := range configKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func LoadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	if err := os.MkdirAll(configDir(), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

func listConfig(cfg *Config) {
	for _, k := range knownConfigKeys() {
		var val string
		switch k {
		case "api-url":
			val = cfg.APIURL
		}
		if val == "" {
			val = "(not set)"
		}
		fmt.Fprintf(os.Stdout, "  %-12s  %s\n", k, val)
	}
}

func ConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage global CLI configuration",
		Long: `Manage global CLI configuration stored in ~/.aether/config.json.

Available config keys:
` + func() string {
			var b strings.Builder
			for _, k := range knownConfigKeys() {
				info := configKeys[k]
				fmt.Fprintf(&b, "  %-12s  %s\n", k, info.Desc)
			}
			return b.String()
		}(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			listConfig(cfg)
			return nil
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			switch args[0] {
			case "api-url":
				fmt.Println(cfg.APIURL)
			default:
				return fmt.Errorf("unknown config key %q — available: %s", args[0], strings.Join(knownConfigKeys(), ", "))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			switch args[0] {
			case "api-url":
				cfg.APIURL = args[1]
			default:
				return fmt.Errorf("unknown config key %q — available: %s", args[0], strings.Join(knownConfigKeys(), ", "))
			}
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			fmt.Fprintf(os.Stdout, "Set %s = %s\n", args[0], args[1])
			return nil
		},
	})

	return cmd
}
