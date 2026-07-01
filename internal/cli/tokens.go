package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListTokens() ([]PAT, error) {
	var tokens []PAT
	if err := c.GetJSON("/api/v1/tokens", &tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

func (c *Client) CreateToken(name string) (*PAT, error) {
	body := map[string]interface{}{"name": name}
	var pat PAT
	if err := c.PostJSON("/api/v1/tokens", body, &pat); err != nil {
		return nil, err
	}
	return &pat, nil
}

func (c *Client) DeleteToken(id string) error {
	return c.DeleteJSON("/api/v1/tokens/" + id)
}

func TokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "Manage personal access tokens",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List tokens",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListTokens()
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
				Short: "Create a token",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					pat, err := cl.CreateToken(name)
					if err != nil {
						return err
					}
					PrintJSON(pat)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Token name (required)")
			c.MarkFlagRequired("name")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a token",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteToken(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
	)

	return cmd
}
