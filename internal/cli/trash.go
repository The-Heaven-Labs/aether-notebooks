package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListTrash() ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	if err := c.GetJSON("/api/v1/trash", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) RestoreFromTrash(resourceType, resourceID string) error {
	body := map[string]interface{}{"resource_type": resourceType, "resource_id": resourceID}
	return c.PostJSON("/api/v1/trash/restore", body, nil)
}

func TrashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "Manage trashed items",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List trashed items",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListTrash()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var resourceType, resourceID string
			c := &cobra.Command{
				Use:   "restore",
				Short: "Restore an item from trash",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					if err := cl.RestoreFromTrash(resourceType, resourceID); err != nil {
						return err
					}
					fmt.Println("Restored.")
					return nil
				},
			}
			c.Flags().StringVar(&resourceType, "type", "", "Resource type (required)")
			c.MarkFlagRequired("type")
			c.Flags().StringVar(&resourceID, "id", "", "Resource ID (required)")
			c.MarkFlagRequired("id")
			return c
		}(),
	)

	return cmd
}
