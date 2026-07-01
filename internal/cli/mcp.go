package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListMCPServers() ([]MCPServer, error) {
	var servers []MCPServer
	if err := c.GetJSON("/api/v1/mcp/servers", &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

func (c *Client) GetMCPServer(id string) (*MCPServer, error) {
	var s MCPServer
	if err := c.GetJSON("/api/v1/mcp/servers/"+id, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) CreateMCPServer(name, mcpType, command string, args []string) (*MCPServer, error) {
	body := map[string]interface{}{
		"name":    name,
		"type":    mcpType,
		"command": command,
	}
	if len(args) > 0 {
		body["args"] = args
	}
	var s MCPServer
	if err := c.PostJSON("/api/v1/mcp/servers", body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) UpdateMCPServer(id, name, mcpType, command string, args []string) (*MCPServer, error) {
	body := map[string]interface{}{}
	if name != "" {
		body["name"] = name
	}
	if mcpType != "" {
		body["type"] = mcpType
	}
	if command != "" {
		body["command"] = command
	}
	if len(args) > 0 {
		body["args"] = args
	}
	var s MCPServer
	if err := c.PutJSON("/api/v1/mcp/servers/"+id, body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) DeleteMCPServer(id string) error {
	return c.DeleteJSON("/api/v1/mcp/servers/" + id)
}

func (c *Client) TestMCPServer(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.PostJSON("/api/v1/mcp/servers/"+id+"/test", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func MCPServersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp-servers",
		Short: "Manage MCP servers",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List MCP servers",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListMCPServers()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, mcpType, command string
			var argsJSON string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create an MCP server",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					var serverArgs []string
					if argsJSON != "" {
						if err := json.Unmarshal([]byte(argsJSON), &serverArgs); err != nil {
							return fmt.Errorf("invalid args JSON: %w", err)
						}
					}
					s, err := cl.CreateMCPServer(name, mcpType, command, serverArgs)
					if err != nil {
						return err
					}
					PrintJSON(s)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Server name (required)")
			c.MarkFlagRequired("name")
			c.Flags().StringVar(&mcpType, "type", "stdio", "Server type (stdio, url)")
			c.Flags().StringVar(&command, "command", "", "Command (required)")
			c.MarkFlagRequired("command")
			c.Flags().StringVar(&argsJSON, "args", "", "JSON array of command arguments")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get an MCP server",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetMCPServer(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, mcpType, command string
			var argsJSON string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Update an MCP server",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					var serverArgs []string
					if argsJSON != "" {
						if err := json.Unmarshal([]byte(argsJSON), &serverArgs); err != nil {
							return fmt.Errorf("invalid args JSON: %w", err)
						}
					}
					s, err := cl.UpdateMCPServer(args[0], name, mcpType, command, serverArgs)
					if err != nil {
						return err
					}
					PrintJSON(s)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Server name")
			c.Flags().StringVar(&mcpType, "type", "", "Server type")
			c.Flags().StringVar(&command, "command", "", "Command")
			c.Flags().StringVar(&argsJSON, "args", "", "JSON array of command arguments")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete an MCP server",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteMCPServer(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "test <id>",
			Short: "Test an MCP server",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.TestMCPServer(args[0])
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
