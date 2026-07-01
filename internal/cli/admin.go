package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) AdminListOrgs() ([]Org, error) {
	var orgs []Org
	if err := c.GetJSON("/api/v1/admin/orgs", &orgs); err != nil {
		return nil, err
	}
	return orgs, nil
}

func (c *Client) AdminCreateOrg(name, slug string) (*Org, error) {
	body := map[string]interface{}{"name": name, "slug": slug}
	var o Org
	if err := c.PostJSON("/api/v1/admin/orgs", body, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func (c *Client) AdminListUsers() ([]User, error) {
	var users []User
	if err := c.GetJSON("/api/v1/admin/users", &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (c *Client) AdminUpdateUser(id string, isPlatformAdmin bool) error {
	body := map[string]interface{}{"is_platform_admin": isPlatformAdmin}
	return c.PutJSON("/api/v1/admin/users/"+id, body, nil)
}

func (c *Client) AdminListSSOProviders() ([]SSOProvider, error) {
	var providers []SSOProvider
	if err := c.GetJSON("/api/v1/admin/sso/providers", &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

func (c *Client) AdminCreateSSOProvider(name, providerType, clientID, clientSecret, discoveryURL string, allowedDomains []string) (*SSOProvider, error) {
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
	if err := c.PostJSON("/api/v1/admin/sso/providers", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) AdminUpdateSSOProvider(id, name, providerType, clientID, clientSecret, discoveryURL string, allowedDomains []string) (*SSOProvider, error) {
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
	if err := c.PutJSON("/api/v1/admin/sso/providers/"+id, body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) AdminDeleteSSOProvider(id string) error {
	return c.DeleteJSON("/api/v1/admin/sso/providers/" + id)
}

func AdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Platform admin commands",
	}

	// orgs subcommand
	cmd.AddCommand(
		func() *cobra.Command {
			var orgsCmd = &cobra.Command{
				Use:   "orgs",
				Short: "Manage organizations",
			}

			orgsCmd.AddCommand(
				&cobra.Command{
					Use:   "list",
					Short: "List all organizations",
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.AdminListOrgs()
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				func() *cobra.Command {
					var name, slug string
					c := &cobra.Command{
						Use:   "create",
						Short: "Create an organization",
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							o, err := cl.AdminCreateOrg(name, slug)
							if err != nil {
								return err
							}
							PrintJSON(o)
							return nil
						},
					}
					c.Flags().StringVarP(&name, "name", "n", "", "Organization name (required)")
					c.MarkFlagRequired("name")
					c.Flags().StringVarP(&slug, "slug", "s", "", "Organization slug (required)")
					c.MarkFlagRequired("slug")
					return c
				}(),
			)
			return orgsCmd
		}(),
		// users subcommand
		func() *cobra.Command {
			var usersCmd = &cobra.Command{
				Use:   "users",
				Short: "Manage users",
			}

			usersCmd.AddCommand(
				&cobra.Command{
					Use:   "list",
					Short: "List all users",
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.AdminListUsers()
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				func() *cobra.Command {
					var isPlatformAdmin bool
					c := &cobra.Command{
						Use:   "update <id>",
						Short: "Update a user (set platform admin)",
						Args:  cobra.ExactArgs(1),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							if err := cl.AdminUpdateUser(args[0], isPlatformAdmin); err != nil {
								return err
							}
							fmt.Println("User updated.")
							return nil
						},
					}
					c.Flags().BoolVar(&isPlatformAdmin, "admin", false, "Set as platform admin")
					return c
				}(),
			)
			return usersCmd
		}(),
		// sso subcommand
		func() *cobra.Command {
			var ssoCmd = &cobra.Command{
				Use:   "sso",
				Short: "Manage platform SSO providers",
			}

			ssoCmd.AddCommand(
				&cobra.Command{
					Use:   "list",
					Short: "List platform SSO providers",
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.AdminListSSOProviders()
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
						Short: "Create a platform SSO provider",
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
							p, err := cl.AdminCreateSSOProvider(name, providerType, clientID, clientSecret, discoveryURL, domains)
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
						Short: "Update a platform SSO provider",
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
							p, err := cl.AdminUpdateSSOProvider(args[0], name, providerType, clientID, clientSecret, discoveryURL, domains)
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
					Short: "Delete a platform SSO provider",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.AdminDeleteSSOProvider(args[0]); err != nil {
							return err
						}
						fmt.Println("Deleted.")
						return nil
					},
				},
			)
			return ssoCmd
		}(),
	)

	return cmd
}
