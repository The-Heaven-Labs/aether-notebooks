package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListAuditLogs() ([]AuditLog, error) {
	var resp struct {
		Entries []AuditLog `json:"entries"`
		Total   int        `json:"total"`
	}
	if err := c.GetJSON("/api/v1/audit", &resp); err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

func (c *Client) OrgGetAuditS3Config() (map[string]any, error) {
	var cfg map[string]any
	if err := c.GetJSON("/api/v1/audit/s3-config", &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Client) OrgSetAuditS3Config(cfg map[string]any) error {
	return c.PutJSON("/api/v1/audit/s3-config", cfg, nil)
}

func (c *Client) OrgTestAuditS3Config() error {
	return c.PostJSON("/api/v1/audit/s3-config/test", map[string]any{}, nil)
}

func AuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit log operations",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
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
	})

	// s3-config subcommand
	cmd.AddCommand(
		func() *cobra.Command {
			var endpoint, region, bucket, accessKey, secretKey string
			var useRole bool
			var batchSize, flushInterval int

			s3Cmd := &cobra.Command{
				Use:   "s3-config",
				Short: "Manage org audit S3 export config",
			}

			s3Cmd.AddCommand(&cobra.Command{
				Use:   "get",
				Short: "Show current org audit S3 config",
				RunE: func(cmd *cobra.Command, args []string) error {
					c, err := LoadClient()
					if err != nil {
						return err
					}
					cfg, err := c.OrgGetAuditS3Config()
					if err != nil {
						return err
					}
					PrintJSON(cfg)
					return nil
				},
			})

			setCmd := &cobra.Command{
				Use:   "set",
				Short: "Set org audit S3 config",
				RunE: func(cmd *cobra.Command, args []string) error {
					c, err := LoadClient()
					if err != nil {
						return err
					}
					body := map[string]any{
						"endpoint":           endpoint,
						"region":             region,
						"bucket":             bucket,
						"access_key":         accessKey,
						"secret_key":         secretKey,
						"use_role":           useRole,
						"batch_size":         batchSize,
						"flush_interval_secs": flushInterval,
						"enabled":            true,
					}
					if err := c.OrgSetAuditS3Config(body); err != nil {
						return err
					}
					fmt.Println("Org audit S3 config updated.")
					return nil
				},
			}
			setCmd.Flags().StringVar(&endpoint, "endpoint", "", "S3 endpoint (leave empty for AWS)")
			setCmd.Flags().StringVar(&region, "region", "us-east-1", "S3 region")
			setCmd.Flags().StringVar(&bucket, "bucket", "", "S3 bucket (required)")
			setCmd.Flags().StringVar(&accessKey, "access-key", "", "S3 access key")
			setCmd.Flags().StringVar(&secretKey, "secret-key", "", "S3 secret key")
			setCmd.Flags().BoolVar(&useRole, "use-role", false, "Use IAM role instead of keys")
			setCmd.Flags().IntVar(&batchSize, "batch-size", 100, "Batch size")
			setCmd.Flags().IntVar(&flushInterval, "flush-interval", 60, "Flush interval (seconds)")
			setCmd.MarkFlagRequired("bucket")
			s3Cmd.AddCommand(setCmd)

			s3Cmd.AddCommand(&cobra.Command{
				Use:   "test",
				Short: "Test S3 connection",
				RunE: func(cmd *cobra.Command, args []string) error {
					c, err := LoadClient()
					if err != nil {
						return err
					}
					if err := c.OrgTestAuditS3Config(); err != nil {
						return err
					}
					fmt.Println("Connection successful.")
					return nil
				},
			})

			return s3Cmd
		}(),
	)

	return cmd
}
