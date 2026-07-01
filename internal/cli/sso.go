package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListOrgSSOProviders() ([]SSOProvider, error) {
	var providers []SSOProvider
	if err := c.GetJSON("/api/v1/sso/providers", &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

func (c *Client) CreateOrgSSOProvider(name, providerType, clientID, clientSecret, discoveryURL string, allowedDomains []string) (*SSOProvider, error) {
	body := map[string]interface{}{
		"name":          name,
		"provider_type": providerType,
		"client_id":     clientID,
		"client_secret": clientSecret,
		"discovery_url": discoveryURL,
	}
	if len(allowedDomains) > 0 {
		body["allowed_domains"] = allowedDomains
	}
	var p SSOProvider
	if err := c.PostJSON("/api/v1/sso/providers", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) UpdateOrgSSOProvider(id, name, providerType, clientID, clientSecret, discoveryURL string, allowedDomains []string) (*SSOProvider, error) {
	body := map[string]interface{}{}
	if name != "" {
		body["name"] = name
	}
	if providerType != "" {
		body["provider_type"] = providerType
	}
	if clientID != "" {
		body["client_id"] = clientID
	}
	if clientSecret != "" {
		body["client_secret"] = clientSecret
	}
	if discoveryURL != "" {
		body["discovery_url"] = discoveryURL
	}
	if len(allowedDomains) > 0 {
		body["allowed_domains"] = allowedDomains
	}
	var p SSOProvider
	if err := c.PutJSON("/api/v1/sso/providers/"+id, body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) DeleteOrgSSOProvider(id string) error {
	return c.DeleteJSON("/api/v1/sso/providers/" + id)
}

func (c *Client) ListPlatformSSOProviders() ([]SSOProvider, error) {
	var providers []SSOProvider
	if err := c.GetJSON("/api/v1/sso/platform-providers", &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

func (c *Client) EnablePlatformSSOProvider(id string) error {
	return c.PostJSON("/api/v1/sso/platform-providers/"+id+"/enable", nil, nil)
}

func (c *Client) DisablePlatformSSOProvider(id string) error {
	return c.DeleteJSON("/api/v1/sso/platform-providers/" + id + "/enable")
}

func (c *Client) GetSSOSettings() (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/sso/settings", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateSSOSettings(settings map[string]interface{}) error {
	return c.PutJSON("/api/v1/sso/settings", settings, nil)
}

func SSOCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sso",
		Short: "Manage SSO providers and settings",
	}

	// org-level providers
	cmd.AddCommand(
		func() *cobra.Command {
			var providersCmd = &cobra.Command{
				Use:   "providers",
				Short: "Manage org SSO providers",
			}

			providersCmd.AddCommand(
				&cobra.Command{
					Use:   "list",
					Short: "List org SSO providers",
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.ListOrgSSOProviders()
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				func() *cobra.Command {
					var name, providerType, clientID, clientSecret, discoveryURL string
					var allowedDomainsJSON string
					c := &cobra.Command{
						Use:   "create",
						Short: "Create an org SSO provider",
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							var domains []string
							if allowedDomainsJSON != "" {
								if err := json.Unmarshal([]byte(allowedDomainsJSON), &domains); err != nil {
									return fmt.Errorf("invalid allowed-domains JSON: %w", err)
								}
							}
							p, err := cl.CreateOrgSSOProvider(name, providerType, clientID, clientSecret, discoveryURL, domains)
							if err != nil {
								return err
							}
							PrintJSON(p)
							return nil
						},
					}
					c.Flags().StringVarP(&name, "name", "n", "", "Provider name (required)")
					c.MarkFlagRequired("name")
					c.Flags().StringVar(&providerType, "type", "oidc", "Provider type")
					c.Flags().StringVar(&clientID, "client-id", "", "Client ID (required)")
					c.MarkFlagRequired("client-id")
					c.Flags().StringVar(&clientSecret, "client-secret", "", "Client secret (required)")
					c.MarkFlagRequired("client-secret")
					c.Flags().StringVar(&discoveryURL, "discovery-url", "", "Discovery URL (required)")
					c.MarkFlagRequired("discovery-url")
					c.Flags().StringVar(&allowedDomainsJSON, "allowed-domains", "", "JSON array of allowed domains")
					return c
				}(),
				func() *cobra.Command {
					var name, providerType, clientID, clientSecret, discoveryURL string
					var allowedDomainsJSON string
					c := &cobra.Command{
						Use:   "update <id>",
						Short: "Update an org SSO provider",
						Args:  cobra.ExactArgs(1),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							var domains []string
							if allowedDomainsJSON != "" {
								if err := json.Unmarshal([]byte(allowedDomainsJSON), &domains); err != nil {
									return fmt.Errorf("invalid allowed-domains JSON: %w", err)
								}
							}
							p, err := cl.UpdateOrgSSOProvider(args[0], name, providerType, clientID, clientSecret, discoveryURL, domains)
							if err != nil {
								return err
							}
							PrintJSON(p)
							return nil
						},
					}
					c.Flags().StringVarP(&name, "name", "n", "", "Provider name")
					c.Flags().StringVar(&providerType, "type", "", "Provider type")
					c.Flags().StringVar(&clientID, "client-id", "", "Client ID")
					c.Flags().StringVar(&clientSecret, "client-secret", "", "Client secret")
					c.Flags().StringVar(&discoveryURL, "discovery-url", "", "Discovery URL")
					c.Flags().StringVar(&allowedDomainsJSON, "allowed-domains", "", "JSON array of allowed domains")
					return c
				}(),
				&cobra.Command{
					Use:   "delete <id>",
					Short: "Delete an org SSO provider",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.DeleteOrgSSOProvider(args[0]); err != nil {
							return err
						}
						fmt.Println("Deleted.")
						return nil
					},
				},
			)
			return providersCmd
		}(),
		// platform providers
		func() *cobra.Command {
			var platformCmd = &cobra.Command{
				Use:   "platform-providers",
				Short: "Manage platform SSO providers",
			}

			platformCmd.AddCommand(
				&cobra.Command{
					Use:   "list",
					Short: "List platform SSO providers",
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.ListPlatformSSOProviders()
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				&cobra.Command{
					Use:   "enable <id>",
					Short: "Enable a platform SSO provider for the org",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.EnablePlatformSSOProvider(args[0]); err != nil {
							return err
						}
						fmt.Println("Enabled.")
						return nil
					},
				},
				&cobra.Command{
					Use:   "disable <id>",
					Short: "Disable a platform SSO provider for the org",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.DisablePlatformSSOProvider(args[0]); err != nil {
							return err
						}
						fmt.Println("Disabled.")
						return nil
					},
				},
			)
			return platformCmd
		}(),
		// sso settings
		func() *cobra.Command {
			var settingsCmd = &cobra.Command{
				Use:   "settings",
				Short: "Manage SSO settings",
			}

			settingsCmd.AddCommand(
				&cobra.Command{
					Use:   "get",
					Short: "Get SSO settings",
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.GetSSOSettings()
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				func() *cobra.Command {
					var settingsJSON string
					c := &cobra.Command{
						Use:   "update",
						Short: "Update SSO settings",
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							var settings map[string]interface{}
							if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
								return fmt.Errorf("invalid settings JSON: %w", err)
							}
							if err := cl.UpdateSSOSettings(settings); err != nil {
								return err
							}
							fmt.Println("Settings updated.")
							return nil
						},
					}
					c.Flags().StringVar(&settingsJSON, "settings", "", "JSON settings object (required)")
					c.MarkFlagRequired("settings")
					return c
				}(),
			)
			return settingsCmd
		}(),
	)

	return cmd
}
