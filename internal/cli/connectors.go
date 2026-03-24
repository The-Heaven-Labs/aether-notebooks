package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
	)
	return cmd
}
