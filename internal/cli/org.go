package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) GetOrgSharing() (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/org/sharing", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateOrgSharing(settings map[string]interface{}) error {
	return c.PutJSON("/api/v1/org/sharing", settings, nil)
}

func (c *Client) GetOrgInvitations() (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/org/invitations", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateOrgInvitations(settings map[string]interface{}) error {
	return c.PutJSON("/api/v1/org/invitations", settings, nil)
}

func (c *Client) GetOrgRegistration() (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/org/registration", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateOrgRegistration(settings map[string]interface{}) error {
	return c.PutJSON("/api/v1/org/registration", settings, nil)
}

func OrgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage org settings",
	}

	for _, cfg := range []struct {
		Use   string
		Short string
		Get   func(*Client) (map[string]interface{}, error)
		Set   func(*Client, map[string]interface{}) error
	}{
		{"sharing", "Org sharing settings", func(c *Client) (map[string]interface{}, error) { return c.GetOrgSharing() }, func(c *Client, s map[string]interface{}) error { return c.UpdateOrgSharing(s) }},
		{"invitations", "Org invitation settings", func(c *Client) (map[string]interface{}, error) { return c.GetOrgInvitations() }, func(c *Client, s map[string]interface{}) error { return c.UpdateOrgInvitations(s) }},
		{"registration", "Org registration settings", func(c *Client) (map[string]interface{}, error) { return c.GetOrgRegistration() }, func(c *Client, s map[string]interface{}) error { return c.UpdateOrgRegistration(s) }},
	} {
		section := cfg
		sub := &cobra.Command{
			Use:   section.Use,
			Short: section.Short,
		}
		sub.AddCommand(
			&cobra.Command{
				Use:   "get",
				Short: "Get " + section.Use + " settings",
				RunE: func(cmd *cobra.Command, args []string) error {
					c, err := LoadClient()
					if err != nil {
						return err
					}
					result, err := section.Get(c)
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
					Short: "Update " + section.Use + " settings",
					RunE: func(cmd *cobra.Command, args []string) error {
						cl, err := LoadClient()
						if err != nil {
							return err
						}
						var settings map[string]interface{}
						if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
							return fmt.Errorf("invalid settings JSON: %w", err)
						}
						if err := section.Set(cl, settings); err != nil {
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
		cmd.AddCommand(sub)
	}

	return cmd
}
