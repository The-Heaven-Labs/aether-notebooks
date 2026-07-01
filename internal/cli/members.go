package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListMembers() ([]OrgMember, error) {
	var members []OrgMember
	if err := c.GetJSON("/api/v1/members", &members); err != nil {
		return nil, err
	}
	return members, nil
}

func (c *Client) InviteMember(email, role string) error {
	body := map[string]interface{}{"email": email, "role": role}
	return c.PostJSON("/api/v1/members", body, nil)
}

func (c *Client) CreateInviteLink(role string) (map[string]string, error) {
	body := map[string]interface{}{"role": role}
	var result map[string]string
	if err := c.PostJSON("/api/v1/members/invite-link", body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateMemberRole(userID, role string) error {
	body := map[string]interface{}{"role": role}
	return c.PutJSON("/api/v1/members/"+userID, body, nil)
}

func (c *Client) RemoveMember(userID string) error {
	return c.DeleteJSON("/api/v1/members/" + userID)
}

func MembersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Manage org members",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List org members",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListMembers()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var email, role string
			c := &cobra.Command{
				Use:   "invite",
				Short: "Invite a member to the org",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					if err := cl.InviteMember(email, role); err != nil {
						return err
					}
					fmt.Println("Invited.")
					return nil
				},
			}
			c.Flags().StringVar(&email, "email", "", "Email address (required)")
			c.MarkFlagRequired("email")
			c.Flags().StringVar(&role, "role", "editor", "Role (admin, editor, viewer)")
			return c
		}(),
		func() *cobra.Command {
			var role string
			c := &cobra.Command{
				Use:   "invite-link",
				Short: "Create an invite link",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					result, err := cl.CreateInviteLink(role)
					if err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVar(&role, "role", "editor", "Role (admin, editor, viewer)")
			return c
		}(),
		func() *cobra.Command {
			var role string
			c := &cobra.Command{
				Use:   "update-role <user-id>",
				Short: "Update a member's role",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					if err := cl.UpdateMemberRole(args[0], role); err != nil {
						return err
					}
					fmt.Println("Role updated.")
					return nil
				},
			}
			c.Flags().StringVar(&role, "role", "editor", "New role (admin, editor, viewer)")
			return c
		}(),
		&cobra.Command{
			Use:   "remove <user-id>",
			Short: "Remove a member from the org",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.RemoveMember(args[0]); err != nil {
					return err
				}
				fmt.Println("Removed.")
				return nil
			},
		},
	)

	return cmd
}
