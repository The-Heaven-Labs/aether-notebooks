package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) AdminListOrgs() ([]Org, error) {
	var resp struct {
		Orgs []Org `json:"orgs"`
	}
	if err := c.GetJSON("/api/v1/admin/orgs", &resp); err != nil {
		return nil, err
	}
	return resp.Orgs, nil
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
	var resp struct {
		Users []User `json:"users"`
	}
	if err := c.GetJSON("/api/v1/admin/users", &resp); err != nil {
		return nil, err
	}
	return resp.Users, nil
}

func (c *Client) AdminUpdateUser(id string, isPlatformAdmin bool) error {
	body := map[string]interface{}{"is_platform_admin": isPlatformAdmin}
	return c.PutJSON("/api/v1/admin/users/"+id, body, nil)
}

func (c *Client) AdminGetAuditS3Config() (map[string]any, error) {
	var cfg map[string]any
	if err := c.GetJSON("/api/v1/admin/audit/s3-config", &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Client) AdminSetAuditS3Config(cfg map[string]any) error {
	return c.PutJSON("/api/v1/admin/audit/s3-config", cfg, nil)
}

func (c *Client) AdminTestAuditS3Config() error {
	return c.PostJSON("/api/v1/admin/audit/s3-config/test", map[string]any{}, nil)
}

func (c *Client) AdminListSSOProviders() ([]SSOProvider, error) {
	var resp struct {
		Providers []SSOProvider `json:"providers"`
	}
	if err := c.GetJSON("/api/v1/admin/sso/providers", &resp); err != nil {
		return nil, err
	}
	return resp.Providers, nil
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

	// audit-s3 subcommand
	cmd.AddCommand(
		func() *cobra.Command {
			var endpoint, region, bucket, accessKey, secretKey string
			var useRole bool
			var batchSize, flushInterval int

			auditS3Cmd := &cobra.Command{
				Use:   "audit-s3",
				Short: "Manage platform audit S3 export",
			}

			auditS3Cmd.AddCommand(&cobra.Command{
				Use:   "get",
				Short: "Show current audit S3 config",
				RunE: func(cmd *cobra.Command, args []string) error {
					c, err := LoadClient()
					if err != nil {
						return err
					}
					cfg, err := c.AdminGetAuditS3Config()
					if err != nil {
						return err
					}
					PrintJSON(cfg)
					return nil
				},
			})

			setCmd := &cobra.Command{
				Use:   "set",
				Short: "Set audit S3 config",
				RunE: func(cmd *cobra.Command, args []string) error {
					c, err := LoadClient()
					if err != nil {
						return err
					}
					body := map[string]any{
						"endpoint":           endpoint,
						"region":             region,
						"bucket":             bucket,
						"access_key":         accessKey,
						"secret_key":         secretKey,
						"use_role":           useRole,
						"batch_size":         batchSize,
						"flush_interval_secs": flushInterval,
						"enabled":            true,
					}
					if err := c.AdminSetAuditS3Config(body); err != nil {
						return err
					}
					fmt.Println("Audit S3 config updated.")
					return nil
				},
			}
			setCmd.Flags().StringVar(&endpoint, "endpoint", "", "S3 endpoint (leave empty for AWS)")
			setCmd.Flags().StringVar(&region, "region", "us-east-1", "S3 region")
			setCmd.Flags().StringVar(&bucket, "bucket", "", "S3 bucket (required)")
			setCmd.Flags().StringVar(&accessKey, "access-key", "", "S3 access key")
			setCmd.Flags().StringVar(&secretKey, "secret-key", "", "S3 secret key")
			setCmd.Flags().BoolVar(&useRole, "use-role", false, "Use IAM role instead of keys")
			setCmd.Flags().IntVar(&batchSize, "batch-size", 100, "Batch size")
			setCmd.Flags().IntVar(&flushInterval, "flush-interval", 60, "Flush interval (seconds)")
			setCmd.MarkFlagRequired("bucket")
			auditS3Cmd.AddCommand(setCmd)

			auditS3Cmd.AddCommand(&cobra.Command{
				Use:   "test",
				Short: "Test S3 connection",
				RunE: func(cmd *cobra.Command, args []string) error {
					c, err := LoadClient()
					if err != nil {
						return err
					}
					if err := c.AdminTestAuditS3Config(); err != nil {
						return err
					}
					fmt.Println("Connection successful.")
					return nil
				},
			})

			return auditS3Cmd
		}(),
	)

	return cmd
}
