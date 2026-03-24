package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NotebooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "notebooks",
		Aliases: []string{"nb"},
		Short:   "Manage notebooks",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List notebooks",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				var result interface{}
				if err := c.GetJSON("/api/v1/notebooks", &result); err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var title string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a notebook",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					var result interface{}
					if err := cl.PostJSON("/api/v1/notebooks", map[string]string{"title": title}, &result); err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVarP(&title, "title", "t", "", "Notebook title (required)")
			c.MarkFlagRequired("title")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get a notebook",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				var result interface{}
				if err := c.GetJSON("/api/v1/notebooks/"+args[0], &result); err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a notebook",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteJSON("/api/v1/notebooks/" + args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
	)
	return cmd
}
