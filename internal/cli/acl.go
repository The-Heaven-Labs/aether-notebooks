package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) GetACL(resourceType, resourceID string) ([]ACLEntry, error) {
	var entries []ACLEntry
	if err := c.GetJSON("/api/v1/acl/"+resourceType+"/"+resourceID, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (c *Client) SetACL(resourceType, resourceID string, entries []ACLEntry) error {
	body := map[string]interface{}{"entries": entries}
	data, status, err := c.Do("PUT", "/api/v1/acl/"+resourceType+"/"+resourceID, body)
	if err != nil {
		return err
	}
	if status >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &e)
		return fmt.Errorf("API error %d: %s", status, e.Error)
	}
	return nil
}

func ACLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acl",
		Short: "Manage ACL entries",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "get <resource-type> <resource-id>",
			Short: "Get ACL entries for a resource",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetACL(args[0], args[1])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var entriesJSON string
			c := &cobra.Command{
				Use:   "set <resource-type> <resource-id>",
				Short: "Set ACL entries for a resource",
				Args:  cobra.ExactArgs(2),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					var entries []ACLEntry
					if err := json.Unmarshal([]byte(entriesJSON), &entries); err != nil {
						return fmt.Errorf("invalid entries JSON: %w", err)
					}
					if err := cl.SetACL(args[0], args[1], entries); err != nil {
						return err
					}
					fmt.Println("ACL updated.")
					return nil
				},
			}
			c.Flags().StringVar(&entriesJSON, "entries", "", "JSON array of ACL entries (required)")
			c.MarkFlagRequired("entries")
			return c
		}(),
	)

	return cmd
}
