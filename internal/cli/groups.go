package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListGroups() ([]Group, error) {
	var groups []Group
	if err := c.GetJSON("/api/v1/groups", &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func (c *Client) CreateGroup(name string) (*Group, error) {
	body := map[string]interface{}{"name": name}
	var g Group
	if err := c.PostJSON("/api/v1/groups", body, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (c *Client) UpdateGroup(id, name string) (*Group, error) {
	body := map[string]interface{}{"name": name}
	var g Group
	if err := c.PutJSON("/api/v1/groups/"+id, body, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (c *Client) DeleteGroup(id string) error {
	return c.DeleteJSON("/api/v1/groups/" + id)
}

func (c *Client) ListGroupMembers(id string) ([]GroupMember, error) {
	var members []GroupMember
	if err := c.GetJSON("/api/v1/groups/"+id+"/members", &members); err != nil {
		return nil, err
	}
	return members, nil
}

func (c *Client) AddGroupMember(groupID, userID string) error {
	body := map[string]interface{}{"user_id": userID}
	return c.PostJSON("/api/v1/groups/"+groupID+"/members", body, nil)
}

func (c *Client) RemoveGroupMember(groupID, userID string) error {
	return c.DeleteJSON("/api/v1/groups/" + groupID + "/members/" + userID)
}

func GroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Manage groups and group members",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List groups",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListGroups()
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
				Short: "Create a group",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					g, err := cl.CreateGroup(name)
					if err != nil {
						return err
					}
					PrintJSON(g)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Group name (required)")
			c.MarkFlagRequired("name")
			return c
		}(),
		func() *cobra.Command {
			var name string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Rename a group",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					g, err := cl.UpdateGroup(args[0], name)
					if err != nil {
						return err
					}
					PrintJSON(g)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "New group name (required)")
			c.MarkFlagRequired("name")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a group",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteGroup(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		func() *cobra.Command {
			var memberCmd = &cobra.Command{
				Use:   "members",
				Short: "Manage group members",
			}

			memberCmd.AddCommand(
				&cobra.Command{
					Use:   "list <group-id>",
					Short: "List group members",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.ListGroupMembers(args[0])
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				func() *cobra.Command {
					var userID string
					c := &cobra.Command{
						Use:   "add <group-id>",
						Short: "Add a member to a group",
						Args:  cobra.ExactArgs(1),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							if err := cl.AddGroupMember(args[0], userID); err != nil {
								return err
							}
							fmt.Println("Member added.")
							return nil
						},
					}
					c.Flags().StringVar(&userID, "user", "", "User ID (required)")
					c.MarkFlagRequired("user")
					return c
				}(),
				&cobra.Command{
					Use:   "remove <group-id> <user-id>",
					Short: "Remove a member from a group",
					Args:  cobra.ExactArgs(2),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.RemoveGroupMember(args[0], args[1]); err != nil {
							return err
						}
						fmt.Println("Member removed.")
						return nil
					},
				},
			)
			return memberCmd
		}(),
	)

	return cmd
}
