package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListMOTD() ([]MOTD, error) {
	var msgs []MOTD
	if err := c.GetJSON("/api/v1/motd", &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (c *Client) ListMOTDAdmin() ([]MOTD, error) {
	var msgs []MOTD
	if err := c.GetJSON("/api/v1/admin/motd", &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (c *Client) CreateMOTD(content, title string, priority int, pages []string, showOnLogin bool) (*MOTD, error) {
	body := map[string]interface{}{"content": content}
	if title != "" {
		body["title"] = title
	}
	if priority > 0 {
		body["priority"] = priority
	}
	if len(pages) > 0 {
		body["pages"] = pages
	}
	if showOnLogin {
		body["show_on_login"] = true
	}
	var motd MOTD
	if err := c.PostJSON("/api/v1/admin/motd", body, &motd); err != nil {
		return nil, err
	}
	return &motd, nil
}

func (c *Client) UpdateMOTD(id, content, title string, priority int, pages []string, showOnLogin bool) error {
	body := map[string]interface{}{}
	if content != "" {
		body["content"] = content
	}
	if title != "" {
		body["title"] = title
	}
	if priority > 0 {
		body["priority"] = priority
	}
	if len(pages) > 0 {
		body["pages"] = pages
	}
	if showOnLogin {
		body["show_on_login"] = true
	}
	return c.PutJSON("/api/v1/admin/motd/"+id, body, nil)
}

func (c *Client) DeleteMOTD(id string) error {
	return c.DeleteJSON("/api/v1/admin/motd/" + id)
}

func MOTDCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "motd",
		Short: "Manage Message of the Day",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List active MOTD messages",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListMOTD()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var adminCmd = &cobra.Command{
				Use:   "admin",
				Short: "Administer MOTD messages",
			}

			adminCmd.AddCommand(
				&cobra.Command{
					Use:   "list",
					Short: "List all MOTD messages (admin)",
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.ListMOTDAdmin()
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				func() *cobra.Command {
					var content, title string
					var priority int
					var pagesJSON, showOnLogin string
					c := &cobra.Command{
						Use:   "create",
						Short: "Create an MOTD message",
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							var pages []string
							if pagesJSON != "" {
								if err := json.Unmarshal([]byte(pagesJSON), &pages); err != nil {
									return fmt.Errorf("invalid pages JSON: %w", err)
								}
							}
							show := showOnLogin == "true"
							motd, err := cl.CreateMOTD(content, title, priority, pages, show)
							if err != nil {
								return err
							}
							PrintJSON(motd)
							return nil
						},
					}
					c.Flags().StringVar(&content, "content", "", "MOTD content (required)")
					c.MarkFlagRequired("content")
					c.Flags().StringVar(&title, "title", "", "MOTD title")
					c.Flags().IntVar(&priority, "priority", 0, "Priority (higher = more important)")
					c.Flags().StringVar(&pagesJSON, "pages", "", "JSON array of page paths")
					c.Flags().StringVar(&showOnLogin, "show-on-login", "false", "Show on login (true/false)")
					return c
				}(),
				func() *cobra.Command {
					var content, title string
					var priority int
					var pagesJSON, showOnLogin string
					c := &cobra.Command{
						Use:   "update <id>",
						Short: "Update an MOTD message",
						Args:  cobra.ExactArgs(1),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							var pages []string
							if pagesJSON != "" {
								if err := json.Unmarshal([]byte(pagesJSON), &pages); err != nil {
									return fmt.Errorf("invalid pages JSON: %w", err)
								}
							}
							show := showOnLogin == "true"
							if err := cl.UpdateMOTD(args[0], content, title, priority, pages, show); err != nil {
								return err
							}
							fmt.Println("Updated.")
							return nil
						},
					}
					c.Flags().StringVar(&content, "content", "", "MOTD content")
					c.Flags().StringVar(&title, "title", "", "MOTD title")
					c.Flags().IntVar(&priority, "priority", 0, "Priority (higher = more important)")
					c.Flags().StringVar(&pagesJSON, "pages", "", "JSON array of page paths")
					c.Flags().StringVar(&showOnLogin, "show-on-login", "", "Show on login (true/false)")
					return c
				}(),
				&cobra.Command{
					Use:   "delete <id>",
					Short: "Delete an MOTD message",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.DeleteMOTD(args[0]); err != nil {
							return err
						}
						fmt.Println("Deleted.")
						return nil
					},
				},
			)
			return adminCmd
		}(),
	)

	return cmd
}
