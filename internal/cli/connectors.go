package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) GetConnector(id string) (*Connector, error) {
	var conn Connector
	if err := c.GetJSON("/api/v1/connectors/"+id, &conn); err != nil {
		return nil, err
	}
	return &conn, nil
}

func (c *Client) CreateConnector(name, connType, configJSON string) (*Connector, error) {
	var config map[string]interface{}
	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	body := map[string]interface{}{
		"name":   name,
		"type":   connType,
		"config": config,
	}
	var conn Connector
	if err := c.PostJSON("/api/v1/connectors", body, &conn); err != nil {
		return nil, err
	}
	return &conn, nil
}

func (c *Client) UpdateConnector(id, name, configJSON string) (*Connector, error) {
	body := map[string]interface{}{}
	if name != "" {
		body["name"] = name
	}
	if configJSON != "" {
		var config map[string]interface{}
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		body["config"] = config
	}
	var conn Connector
	if err := c.PutJSON("/api/v1/connectors/"+id, body, &conn); err != nil {
		return nil, err
	}
	return &conn, nil
}

func (c *Client) TestConnector(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.PostJSON("/api/v1/connectors/"+id+"/test", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) SetDefaultConnector(id string) error {
	body := map[string]interface{}{"is_default": true}
	return c.PutJSON("/api/v1/connectors/"+id, body, nil)
}

func (c *Client) GetConnectorSchema(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/connectors/"+id+"/schema", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ListConnectorDatabases(id string) ([]string, error) {
	var databases []string
	if err := c.GetJSON("/api/v1/connectors/"+id+"/databases", &databases); err != nil {
		return nil, err
	}
	return databases, nil
}

func ConnectorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connectors",
		Short: "Manage data connectors",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List connectors",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				var result interface{}
				if err := c.GetJSON("/api/v1/connectors", &result); err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a connector",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteJSON("/api/v1/connectors/" + args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		func() *cobra.Command {
			var name, connType, config string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a connector",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					conn, err := cl.CreateConnector(name, connType, config)
					if err != nil {
						return err
					}
					PrintJSON(conn)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Connector name (required)")
			c.Flags().StringVarP(&connType, "type", "t", "", "Connector type (required)")
			c.Flags().StringVar(&config, "config", "", "Connector config as JSON string")
			c.MarkFlagRequired("name")
			c.MarkFlagRequired("type")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get a connector",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				conn, err := c.GetConnector(args[0])
				if err != nil {
					return err
				}
				PrintJSON(conn)
				return nil
			},
		},
		func() *cobra.Command {
			var name, config string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Update a connector",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					conn, err := cl.UpdateConnector(args[0], name, config)
					if err != nil {
						return err
					}
					PrintJSON(conn)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "New connector name")
			c.Flags().StringVar(&config, "config", "", "New connector config as JSON string")
			return c
		}(),
		&cobra.Command{
			Use:   "test <id>",
			Short: "Test a connector connection",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.TestConnector(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		&cobra.Command{
			Use:   "set-default <id>",
			Short: "Set a connector as the default",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.SetDefaultConnector(args[0]); err != nil {
					return err
				}
				fmt.Println("Default connector set.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "schema <id>",
			Short: "Get connector schema",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetConnectorSchema(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		&cobra.Command{
			Use:   "databases <id>",
			Short: "List databases for a connector",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListConnectorDatabases(args[0])
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
