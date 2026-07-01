package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListTools() ([]Tool, error) {
	var tools []Tool
	if err := c.GetJSON("/api/v1/tools", &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func (c *Client) GetTool(id string) (*Tool, error) {
	var t Tool
	if err := c.GetJSON("/api/v1/tools/"+id, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) CreateTool(name, description, toolType string) (*Tool, error) {
	body := map[string]interface{}{
		"name":        name,
		"description": description,
		"type":        toolType,
	}
	var t Tool
	if err := c.PostJSON("/api/v1/tools", body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) UpdateTool(id, name, description, toolType string) (*Tool, error) {
	body := map[string]interface{}{}
	if name != "" {
		body["name"] = name
	}
	if description != "" {
		body["description"] = description
	}
	if toolType != "" {
		body["type"] = toolType
	}
	var t Tool
	if err := c.PutJSON("/api/v1/tools/"+id, body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) DeleteTool(id string) error {
	return c.DeleteJSON("/api/v1/tools/" + id)
}

func (c *Client) TestTool(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.PostJSON("/api/v1/tools/"+id+"/test", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func ToolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Manage custom tools",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List tools",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListTools()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, description, toolType string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a tool",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					t, err := cl.CreateTool(name, description, toolType)
					if err != nil {
						return err
					}
					PrintJSON(t)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Tool name (required)")
			c.MarkFlagRequired("name")
			c.Flags().StringVarP(&description, "description", "d", "", "Tool description (required)")
			c.MarkFlagRequired("description")
			c.Flags().StringVar(&toolType, "type", "", "Tool type (required)")
			c.MarkFlagRequired("type")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get a tool",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetTool(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, description, toolType string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Update a tool",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					t, err := cl.UpdateTool(args[0], name, description, toolType)
					if err != nil {
						return err
					}
					PrintJSON(t)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Tool name")
			c.Flags().StringVarP(&description, "description", "d", "", "Tool description")
			c.Flags().StringVar(&toolType, "type", "", "Tool type")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a tool",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteTool(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "test <id>",
			Short: "Test a tool",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.TestTool(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
	)

	return cmd
}
