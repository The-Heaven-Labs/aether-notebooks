package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListAgents() ([]Agent, error) {
	var agents []Agent
	if err := c.GetJSON("/api/v1/agents", &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

func (c *Client) GetAgent(id string) (*Agent, error) {
	var a Agent
	if err := c.GetJSON("/api/v1/agents/"+id, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (c *Client) CreateAgent(name, description string) (*Agent, error) {
	body := map[string]interface{}{"name": name}
	if description != "" {
		body["description"] = description
	}
	var a Agent
	if err := c.PostJSON("/api/v1/agents", body, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (c *Client) UpdateAgent(id, name, description string) (*Agent, error) {
	body := map[string]interface{}{}
	if name != "" {
		body["name"] = name
	}
	if description != "" {
		body["description"] = description
	}
	var a Agent
	if err := c.PutJSON("/api/v1/agents/"+id, body, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (c *Client) DeleteAgent(id string) error {
	return c.DeleteJSON("/api/v1/agents/" + id)
}

func (c *Client) ListSessions(agentID string) ([]AgentSession, error) {
	var sessions []AgentSession
	if err := c.GetJSON("/api/v1/agents/"+agentID+"/sessions", &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (c *Client) CreateSession(agentID string) (*AgentSession, error) {
	var s AgentSession
	if err := c.PostJSON("/api/v1/agents/"+agentID+"/session", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) GetSession(sessionID string) (*AgentSession, error) {
	var s AgentSession
	if err := c.GetJSON("/api/v1/sessions/"+sessionID, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) GetSessionMessages(sessionID string) ([]AgentMessage, error) {
	var msgs []AgentMessage
	if err := c.GetJSON("/api/v1/sessions/"+sessionID+"/messages", &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func AgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage AI agents",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List agents",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListAgents()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, description string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create an agent",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					a, err := cl.CreateAgent(name, description)
					if err != nil {
						return err
					}
					PrintJSON(a)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Agent name (required)")
			c.MarkFlagRequired("name")
			c.Flags().StringVarP(&description, "description", "d", "", "Agent description")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get an agent",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetAgent(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var name, description string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Update an agent",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					a, err := cl.UpdateAgent(args[0], name, description)
					if err != nil {
						return err
					}
					PrintJSON(a)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Agent name")
			c.Flags().StringVarP(&description, "description", "d", "", "Agent description")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete an agent",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteAgent(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		func() *cobra.Command {
			var sessionCmd = &cobra.Command{
				Use:   "sessions",
				Short: "Manage agent sessions",
			}

			sessionCmd.AddCommand(
				&cobra.Command{
					Use:   "list <agent-id>",
					Short: "List sessions for an agent",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.ListSessions(args[0])
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				&cobra.Command{
					Use:   "create <agent-id>",
					Short: "Create a session for an agent",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						cl, err := LoadClient()
						if err != nil {
							return err
						}
						s, err := cl.CreateSession(args[0])
						if err != nil {
							return err
						}
						PrintJSON(s)
						return nil
					},
				},
				&cobra.Command{
					Use:   "get <session-id>",
					Short: "Get a session",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.GetSession(args[0])
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				&cobra.Command{
					Use:   "messages <session-id>",
					Short: "List messages in a session",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.GetSessionMessages(args[0])
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
			)
			return sessionCmd
		}(),
	)

	return cmd
}
