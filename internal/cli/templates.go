package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListTemplates() ([]Template, error) {
	var templates []Template
	if err := c.GetJSON("/api/v1/templates", &templates); err != nil {
		return nil, err
	}
	return templates, nil
}

func (c *Client) CreateTemplate(name string) (*Template, error) {
	body := map[string]interface{}{"name": name}
	var t Template
	if err := c.PostJSON("/api/v1/templates", body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) DeleteTemplate(id string) error {
	return c.DeleteJSON("/api/v1/templates/" + id)
}

func TemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "Manage notebook templates",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List templates",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListTemplates()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a template",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					t, err := cl.CreateTemplate(name)
					if err != nil {
						return err
					}
					PrintJSON(t)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Template name (required)")
			c.MarkFlagRequired("name")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a template",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteTemplate(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
	)

	return cmd
}
