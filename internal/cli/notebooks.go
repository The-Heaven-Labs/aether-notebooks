package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func (c *Client) UpdateNotebook(id, title, description string) (*Notebook, error) {
	body := map[string]interface{}{}
	if title != "" {
		body["title"] = title
	}
	if description != "" {
		body["description"] = description
	}
	var nb Notebook
	if err := c.PutJSON("/api/v1/notebooks/"+id, body, &nb); err != nil {
		return nil, err
	}
	return &nb, nil
}

func (c *Client) CloneNotebook(id string) (*Notebook, error) {
	var nb Notebook
	if err := c.PostJSON("/api/v1/notebooks/"+id+"/clone", nil, &nb); err != nil {
		return nil, err
	}
	return &nb, nil
}

func (c *Client) ExportNotebook(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/notebooks/"+id+"/export", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ImportNotebook(data map[string]interface{}) (*Notebook, error) {
	var nb Notebook
	if err := c.PostJSON("/api/v1/notebooks/import", data, &nb); err != nil {
		return nil, err
	}
	return &nb, nil
}

func (c *Client) GetNotebookShare(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/notebooks/"+id+"/share", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ShareNotebook(id string) error {
	return c.PostJSON("/api/v1/notebooks/"+id+"/share", nil, nil)
}

func (c *Client) RevokeNotebookShare(id string) error {
	return c.DeleteJSON("/api/v1/notebooks/" + id + "/share")
}

func (c *Client) GetNotebookPermissions(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/notebooks/"+id+"/permissions", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ListSnapshots(notebookID string) ([]Snapshot, error) {
	var snapshots []Snapshot
	if err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/snapshots", &snapshots); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func (c *Client) CreateSnapshot(notebookID, label string) (*Snapshot, error) {
	body := map[string]interface{}{"name": label}
	var s Snapshot
	if err := c.PostJSON("/api/v1/notebooks/"+notebookID+"/snapshots", body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) RestoreSnapshot(notebookID, snapshotID string) error {
	return c.PostJSON("/api/v1/notebooks/"+notebookID+"/snapshots/"+snapshotID+"/restore", nil, nil)
}

func (c *Client) SnapshotDiff(notebookID, snapshotID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/snapshots/"+snapshotID+"/diff", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ListSchedules(notebookID string) ([]Schedule, error) {
	var schedules []Schedule
	if err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/schedules", &schedules); err != nil {
		return nil, err
	}
	return schedules, nil
}

func (c *Client) CreateSchedule(notebookID, cronExpr string, enabled bool) (*Schedule, error) {
	body := map[string]interface{}{
		"cron_expression": cronExpr,
		"enabled":         enabled,
	}
	var s Schedule
	if err := c.PostJSON("/api/v1/notebooks/"+notebookID+"/schedules", body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) GetSchedule(id string) (*Schedule, error) {
	var s Schedule
	if err := c.GetJSON("/api/v1/schedules/"+id, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) UpdateSchedule(id, cronExpr string, enabled bool) (*Schedule, error) {
	body := map[string]interface{}{
		"cron_expression": cronExpr,
		"enabled":         enabled,
	}
	var s Schedule
	if err := c.PutJSON("/api/v1/schedules/"+id, body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) DeleteSchedule(id string) error {
	return c.DeleteJSON("/api/v1/schedules/" + id)
}

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
		func() *cobra.Command {
			var title, description string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Update a notebook",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					nb, err := cl.UpdateNotebook(args[0], title, description)
					if err != nil {
						return err
					}
					PrintJSON(nb)
					return nil
				},
			}
			c.Flags().StringVarP(&title, "title", "t", "", "New notebook title")
			c.Flags().StringVarP(&description, "description", "d", "", "New notebook description")
			return c
		}(),
		&cobra.Command{
			Use:   "clone <id>",
			Short: "Clone a notebook",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				nb, err := c.CloneNotebook(args[0])
				if err != nil {
					return err
				}
				PrintJSON(nb)
				return nil
			},
		},
		&cobra.Command{
			Use:   "export <id>",
			Short: "Export a notebook",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ExportNotebook(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var filePath string
			c := &cobra.Command{
				Use:   "import",
				Short: "Import a notebook from a JSON file",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					data, err := os.ReadFile(filePath)
					if err != nil {
						return fmt.Errorf("read file: %w", err)
					}
					var body map[string]interface{}
					if err := json.Unmarshal(data, &body); err != nil {
						return fmt.Errorf("parse JSON: %w", err)
					}
					nb, err := cl.ImportNotebook(body)
					if err != nil {
						return err
					}
					PrintJSON(nb)
					return nil
				},
			}
			c.Flags().StringVarP(&filePath, "file", "f", "", "Path to JSON file (required)")
			c.MarkFlagRequired("file")
			return c
		}(),
		func() *cobra.Command {
			shareCmd := &cobra.Command{
				Use:   "share",
				Short: "Manage notebook sharing",
			}
			shareCmd.AddCommand(
				&cobra.Command{
					Use:   "status <id>",
					Short: "Get share status of a notebook",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.GetNotebookShare(args[0])
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				&cobra.Command{
					Use:   "on <id>",
					Short: "Enable sharing for a notebook",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.ShareNotebook(args[0]); err != nil {
							return err
						}
						fmt.Println("Sharing enabled.")
						return nil
					},
				},
				&cobra.Command{
					Use:   "off <id>",
					Short: "Disable sharing for a notebook",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.RevokeNotebookShare(args[0]); err != nil {
							return err
						}
						fmt.Println("Sharing disabled.")
						return nil
					},
				},
			)
			return shareCmd
		}(),
		&cobra.Command{
			Use:   "permissions <id>",
			Short: "Get notebook permissions",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetNotebookPermissions(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			snapshotsCmd := &cobra.Command{
				Use:   "snapshots",
				Short: "Manage notebook snapshots",
			}
			snapshotsCmd.AddCommand(
				&cobra.Command{
					Use:   "list <notebook-id>",
					Short: "List snapshots for a notebook",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.ListSnapshots(args[0])
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				func() *cobra.Command {
					var label string
					c := &cobra.Command{
						Use:   "create <notebook-id>",
						Short: "Create a snapshot",
						Args:  cobra.ExactArgs(1),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							s, err := cl.CreateSnapshot(args[0], label)
							if err != nil {
								return err
							}
							PrintJSON(s)
							return nil
						},
					}
					c.Flags().StringVarP(&label, "label", "l", "", "Snapshot label (required)")
					c.MarkFlagRequired("label")
					return c
				}(),
				&cobra.Command{
					Use:   "restore <notebook-id> <snapshot-id>",
					Short: "Restore a notebook to a snapshot",
					Args:  cobra.ExactArgs(2),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.RestoreSnapshot(args[0], args[1]); err != nil {
							return err
						}
						fmt.Println("Snapshot restored.")
						return nil
					},
				},
				&cobra.Command{
					Use:   "diff <notebook-id> <snapshot-id>",
					Short: "Show changes between a notebook and a snapshot",
					Args:  cobra.ExactArgs(2),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.SnapshotDiff(args[0], args[1])
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
			)
			return snapshotsCmd
		}(),
		func() *cobra.Command {
			schedulesCmd := &cobra.Command{
				Use:   "schedules",
				Short: "Manage notebook schedules",
			}
			schedulesCmd.AddCommand(
				&cobra.Command{
					Use:   "list <notebook-id>",
					Short: "List schedules for a notebook",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.ListSchedules(args[0])
						if err != nil {
							return err
						}
						PrintJSON(result)
						return nil
					},
				},
				func() *cobra.Command {
					var cron string
					var enabled bool
					c := &cobra.Command{
						Use:   "create <notebook-id>",
						Short: "Create a schedule",
						Args:  cobra.ExactArgs(1),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							s, err := cl.CreateSchedule(args[0], cron, enabled)
							if err != nil {
								return err
							}
							PrintJSON(s)
							return nil
						},
					}
					c.Flags().StringVar(&cron, "cron", "", "Cron expression (required)")
					c.Flags().BoolVar(&enabled, "enabled", true, "Enable the schedule")
					c.MarkFlagRequired("cron")
					return c
				}(),
				&cobra.Command{
					Use:   "get <id>",
					Short: "Get a schedule",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						s, err := c.GetSchedule(args[0])
						if err != nil {
							return err
						}
						PrintJSON(s)
						return nil
					},
				},
				func() *cobra.Command {
					var cron string
					var enabled bool
					c := &cobra.Command{
						Use:   "update <id>",
						Short: "Update a schedule",
						Args:  cobra.ExactArgs(1),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							s, err := cl.UpdateSchedule(args[0], cron, enabled)
							if err != nil {
								return err
							}
							PrintJSON(s)
							return nil
						},
					}
					c.Flags().StringVar(&cron, "cron", "", "New cron expression")
					c.Flags().BoolVar(&enabled, "enabled", true, "Enable the schedule")
					return c
				}(),
				&cobra.Command{
					Use:   "delete <id>",
					Short: "Delete a schedule",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.DeleteSchedule(args[0]); err != nil {
							return err
						}
						fmt.Println("Deleted.")
						return nil
					},
				},
			)
			return schedulesCmd
		}(),
	)
	return cmd
}
