package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// --- Typed Client Methods ---

func (c *Client) ListDashboards() ([]Dashboard, error) {
	var dashboards []Dashboard
	if err := c.GetJSON("/api/v1/dashboards", &dashboards); err != nil {
		return nil, err
	}
	return dashboards, nil
}

func (c *Client) CreateDashboard(title, description string) (*Dashboard, error) {
	body := map[string]interface{}{"title": title}
	if description != "" {
		body["description"] = description
	}
	var dash Dashboard
	if err := c.PostJSON("/api/v1/dashboards", body, &dash); err != nil {
		return nil, err
	}
	return &dash, nil
}

func (c *Client) GetDashboard(id string) (*Dashboard, error) {
	var dash Dashboard
	if err := c.GetJSON("/api/v1/dashboards/"+id, &dash); err != nil {
		return nil, err
	}
	return &dash, nil
}

func (c *Client) UpdateDashboard(id, title, description string) (*Dashboard, error) {
	body := map[string]interface{}{}
	if title != "" {
		body["title"] = title
	}
	if description != "" {
		body["description"] = description
	}
	var dash Dashboard
	if err := c.PutJSON("/api/v1/dashboards/"+id, body, &dash); err != nil {
		return nil, err
	}
	return &dash, nil
}

func (c *Client) DeleteDashboard(id string) error {
	return c.DeleteJSON("/api/v1/dashboards/" + id)
}

func (c *Client) AddWidget(dashboardID, widgetType, title, cellID string) (*Widget, error) {
	body := map[string]interface{}{"type": widgetType, "cell_id": cellID}
	if title != "" {
		body["config"] = map[string]interface{}{"title": title}
	}
	var widget Widget
	if err := c.PostJSON("/api/v1/dashboards/"+dashboardID+"/widgets", body, &widget); err != nil {
		return nil, err
	}
	return &widget, nil
}

func (c *Client) UpdateWidget(dashboardID, widgetID, title string) (*Widget, error) {
	body := map[string]interface{}{
		"layout": map[string]interface{}{"row": 0, "col": 0, "width": 1, "height": 1},
	}
	if title != "" {
		body["config"] = map[string]interface{}{"title": title}
	}
	_, status, err := c.Do("PUT", "/api/v1/dashboards/"+dashboardID+"/widgets/"+widgetID, body)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("API error %d", status)
	}
	return nil, nil
}

func (c *Client) DeleteWidget(dashboardID, widgetID string) error {
	return c.DeleteJSON("/api/v1/dashboards/" + dashboardID + "/widgets/" + widgetID)
}

func (c *Client) GetDashboardShare(dashboardID string) (map[string]interface{}, error) {
	data, status, err := c.Do("GET", "/api/v1/dashboards/"+dashboardID+"/share", nil)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &e)
		return nil, fmt.Errorf("API error %d: %s", status, e.Error)
	}
	if status == 204 || len(data) == 0 {
		return nil, nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ShareDashboard(dashboardID string) error {
	return c.PostJSON("/api/v1/dashboards/"+dashboardID+"/share", nil, nil)
}

func (c *Client) RevokeDashboardShare(dashboardID string) error {
	return c.DeleteJSON("/api/v1/dashboards/" + dashboardID + "/share")
}

func (c *Client) GetDashboardPermissions(dashboardID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.GetJSON("/api/v1/dashboards/"+dashboardID+"/permissions", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// --- CLI Commands ---

func DashboardsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboards",
		Short: "Manage dashboards and widgets",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List dashboards",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListDashboards()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var title, description string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a dashboard",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					dash, err := cl.CreateDashboard(title, description)
					if err != nil {
						return err
					}
					PrintJSON(dash)
					return nil
				},
			}
			c.Flags().StringVarP(&title, "title", "t", "", "Dashboard title (required)")
			c.MarkFlagRequired("title")
			c.Flags().StringVarP(&description, "description", "d", "", "Dashboard description")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get a dashboard",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				var result interface{}
				if err := c.GetJSON("/api/v1/dashboards/"+args[0], &result); err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		func() *cobra.Command {
			var title, description string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Update a dashboard",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					dash, err := cl.UpdateDashboard(args[0], title, description)
					if err != nil {
						return err
					}
					PrintJSON(dash)
					return nil
				},
			}
			c.Flags().StringVarP(&title, "title", "t", "", "Dashboard title")
			c.Flags().StringVarP(&description, "description", "d", "", "Dashboard description")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a dashboard",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteDashboard(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		func() *cobra.Command {
			var widgetCmd = &cobra.Command{
				Use:   "widgets",
				Short: "Manage dashboard widgets",
			}

			widgetCmd.AddCommand(
				// widgets add
				func() *cobra.Command {
					var widgetType, title, cellID string
					c := &cobra.Command{
						Use:   "add <dashboard-id>",
						Short: "Add a widget to a dashboard",
						Args:  cobra.ExactArgs(1),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							widget, err := cl.AddWidget(args[0], widgetType, title, cellID)
							if err != nil {
								return err
							}
							PrintJSON(widget)
							return nil
						},
					}
					c.Flags().StringVar(&widgetType, "type", "chart", "Widget type (chart, table, text, metric, etc.)")
					c.Flags().StringVar(&title, "title", "", "Widget title")
					c.Flags().StringVar(&cellID, "cell", "", "Cell ID (required)")
					c.MarkFlagRequired("cell")
					return c
				}(),
				// widgets update
				func() *cobra.Command {
					var title string
					c := &cobra.Command{
						Use:   "update <dashboard-id> <widget-id>",
						Short: "Update a widget",
						Args:  cobra.ExactArgs(2),
						RunE: func(cmd *cobra.Command, args []string) error {
							cl, err := LoadClient()
							if err != nil {
								return err
							}
							widget, err := cl.UpdateWidget(args[0], args[1], title)
							if err != nil {
								return err
							}
							PrintJSON(widget)
							return nil
						},
					}
					c.Flags().StringVar(&title, "title", "", "Widget title")
					return c
				}(),
				// widgets delete
				&cobra.Command{
					Use:   "delete <dashboard-id> <widget-id>",
					Short: "Delete a widget",
					Args:  cobra.ExactArgs(2),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.DeleteWidget(args[0], args[1]); err != nil {
							return err
						}
						fmt.Println("Deleted.")
						return nil
					},
				},
			)
			return widgetCmd
		}(),
		func() *cobra.Command {
			var shareCmd = &cobra.Command{
				Use:   "share",
				Short: "Manage dashboard sharing",
			}

			shareCmd.AddCommand(
				&cobra.Command{
					Use:   "status <id>",
					Short: "Get dashboard share status",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						result, err := c.GetDashboardShare(args[0])
						if err != nil {
							return err
						}
						if result == nil {
							fmt.Println("No share link.")
							return nil
						}
						PrintJSON(result)
						return nil
					},
				},
				&cobra.Command{
					Use:   "on <id>",
					Short: "Create a public share link for a dashboard",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.ShareDashboard(args[0]); err != nil {
							return err
						}
						fmt.Println("Dashboard shared.")
						return nil
					},
				},
				&cobra.Command{
					Use:   "off <id>",
					Short: "Revoke the public share link for a dashboard",
					Args:  cobra.ExactArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error {
						c, err := LoadClient()
						if err != nil {
							return err
						}
						if err := c.RevokeDashboardShare(args[0]); err != nil {
							return err
						}
						fmt.Println("Share revoked.")
						return nil
					},
				},
			)
			return shareCmd
		}(),
		&cobra.Command{
			Use:   "permissions <id>",
			Short: "Get dashboard permissions",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetDashboardPermissions(args[0])
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
	)

	return cmd
}
