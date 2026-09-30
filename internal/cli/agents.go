package cli

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
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

// CreateSessionOptions collects the inputs for creating an agent session.
// Share references are resolved by the client and sent in the same request as
// the session itself.
type CreateSessionOptions struct {
	NotebookID           string
	ShareUsers           []string
	ShareGroups          []string
	ShareEveryone        bool
	ShareNotebookViewers bool
}

// CreateSession creates a session and applies its shares in a single POST.
func (c *Client) CreateSession(agentID string, opts CreateSessionOptions) (*CreateSessionResult, error) {
	if opts.ShareNotebookViewers && opts.NotebookID == "" {
		return nil, fmt.Errorf("--share-notebook-viewers requires --notebook")
	}

	shares, err := c.resolveSessionShares(opts)
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{"notebook_id": opts.NotebookID}
	if len(shares) > 0 {
		body["shares"] = shares
	}
	if opts.ShareNotebookViewers {
		body["share_with_notebook_viewers"] = true
	}

	var result CreateSessionResult
	if err := c.PostJSON("/api/v1/agents/"+agentID+"/session", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// resolveSessionShares turns --share-user/--share-group references into
// read-only ACL entries: UUIDs are used as-is, while emails and group names are
// resolved against the org and must match exactly one subject.
func (c *Client) resolveSessionShares(opts CreateSessionOptions) ([]ACLEntry, error) {
	if len(opts.ShareUsers) == 0 && len(opts.ShareGroups) == 0 && !opts.ShareEveryone {
		return nil, nil
	}

	shares := make([]ACLEntry, 0, len(opts.ShareUsers)+len(opts.ShareGroups)+1)

	if len(opts.ShareUsers) > 0 {
		var members []OrgMember
		for _, ref := range opts.ShareUsers {
			subjectID := ref
			if !isUUID(ref) {
				if members == nil {
					var err error
					members, err = c.ListMembers()
					if err != nil {
						return nil, err
					}
				}
				var matches []string
				for _, m := range members {
					if strings.EqualFold(m.Email, ref) {
						matches = append(matches, m.UserID)
					}
				}
				switch len(matches) {
				case 0:
					return nil, fmt.Errorf("share user %q not found in org (expected an email or user UUID)", ref)
				case 1:
					subjectID = matches[0]
				default:
					return nil, fmt.Errorf("share user %q is ambiguous: %d org members match (use a user UUID)", ref, len(matches))
				}
			}
			shares = append(shares, ACLEntry{SubjectType: "user", SubjectID: subjectID, Actions: []string{"view"}})
		}
	}

	if len(opts.ShareGroups) > 0 {
		var groups []Group
		for _, ref := range opts.ShareGroups {
			subjectID := ref
			if !isUUID(ref) {
				if groups == nil {
					var err error
					groups, err = c.ListGroups()
					if err != nil {
						return nil, err
					}
				}
				var matches []string
				for _, g := range groups {
					if strings.EqualFold(g.Name, ref) {
						matches = append(matches, g.ID)
					}
				}
				switch len(matches) {
				case 0:
					return nil, fmt.Errorf("share group %q not found in org (expected a name or group UUID)", ref)
				case 1:
					subjectID = matches[0]
				default:
					return nil, fmt.Errorf("share group %q is ambiguous: %d groups match (use a group UUID)", ref, len(matches))
				}
			}
			shares = append(shares, ACLEntry{SubjectType: "group", SubjectID: subjectID, Actions: []string{"view"}})
		}
	}

	if opts.ShareEveryone {
		shares = append(shares, ACLEntry{SubjectType: "org_role", SubjectID: "everyone", Actions: []string{"view"}})
	}

	return shares, nil
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// sessionShareSummary renders the raw share references for the create command's
// human-readable output; the API still receives resolved subject IDs.
func sessionShareSummary(users, groups []string, everyone bool) string {
	parts := make([]string, 0, len(users)+len(groups)+1)
	parts = append(parts, users...)
	for _, g := range groups {
		parts = append(parts, "group "+g)
	}
	if everyone {
		parts = append(parts, "everyone in the org")
	}
	return strings.Join(parts, ", ")
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
				func() *cobra.Command {
					var (
						notebookID           string
						shareUsers           []string
						shareGroups          []string
						shareEveryone        bool
						shareNotebookViewers bool
					)
					c := &cobra.Command{
						Use:   "create <agent-id>",
						Short: "Create a session for an agent",
						Args:  cobra.ExactArgs(1),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							result, err := cl.CreateSession(args[0], CreateSessionOptions{
								NotebookID:           notebookID,
								ShareUsers:           shareUsers,
								ShareGroups:          shareGroups,
								ShareEveryone:        shareEveryone,
								ShareNotebookViewers: shareNotebookViewers,
							})
							if err != nil {
								return err
							}
							fmt.Printf("Session created: %s\n", result.SessionID)
							fmt.Printf("Context window: %d\n", result.ContextWindow)
							if summary := sessionShareSummary(shareUsers, shareGroups, shareEveryone); summary != "" {
								fmt.Printf("Shared with: %s\n", summary)
							}
							if shareNotebookViewers {
								fmt.Println("Notebook viewers can read this session.")
							}
							return nil
						},
					}
					c.Flags().StringVar(&notebookID, "notebook", "", "Notebook ID (required)")
					c.MarkFlagRequired("notebook")
					c.Flags().StringArrayVar(&shareUsers, "share-user", nil, "Share with an org member by email or user UUID (repeatable)")
					c.Flags().StringArrayVar(&shareGroups, "share-group", nil, "Share with a group by name or group UUID (repeatable)")
					c.Flags().BoolVar(&shareEveryone, "share-everyone", false, "Share with everyone in the org")
					c.Flags().BoolVar(&shareNotebookViewers, "share-notebook-viewers", false, "Allow anyone who can view the notebook to read this session")
					return c
				}(),
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
