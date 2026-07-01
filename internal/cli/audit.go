package cli

import (
	"github.com/spf13/cobra"
)

func (c *Client) ListAuditLogs() ([]AuditLog, error) {
	var logs []AuditLog
	if err := c.GetJSON("/api/v1/audit", &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func AuditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit",
		Short: "List audit logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := LoadClient()
			if err != nil {
				return err
			}
			result, err := c.ListAuditLogs()
			if err != nil {
				return err
			}
			PrintJSON(result)
			return nil
		},
	}
}
