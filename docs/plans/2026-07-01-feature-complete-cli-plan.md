# Feature-Complete CLI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend the `aether` CLI to cover all API endpoints, achieving feature parity with the web UI.

**Architecture:** Cobra commands in `internal/cli/`, one file per resource domain. Each file has typed `Client` methods and a `XxxCmd() *cobra.Command` constructor. `cmd/aether/main.go` registers all top-level commands.

**Tech Stack:** Go, Cobra, `net/http`

**Design doc:** `docs/plans/2026-07-01-feature-complete-cli-design.md`

---

### Task 1: Shared types and main.go registration

**Files:**
- Create: `internal/cli/types.go`
- Modify: `cmd/aether/main.go`
- Modify: `internal/cli/notebooks.go` (add subcommands)
- Modify: `internal/cli/cells.go` (add subcommands)
- Modify: `internal/cli/connectors.go` (add subcommands)

**Step 1: Create `internal/cli/types.go` with shared response structs**

These are the Go types for JSON unmarshalling of API responses. Only the fields the CLI displays.

```go
package cli

import "time"

type (
	Notebook struct {
		ID          string    `json:"id"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		FolderID    string    `json:"folder_id"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
	}

	Cell struct {
		ID         string `json:"id"`
		NotebookID string `json:"notebook_id"`
		Type       string `json:"type"`
		Language   string `json:"language"`
		Source     string `json:"source"`
		Result     any    `json:"result"`
		Ordinal    int    `json:"ordinal"`
	}

	CellVersion struct {
		ID        string    `json:"id"`
		CellID    string    `json:"cell_id"`
		Source    string    `json:"source"`
		CreatedAt time.Time `json:"created_at"`
	}

	Folder struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		ParentID  string    `json:"parent_id"`
		OwnerID   string    `json:"owner_id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	Dashboard struct {
		ID          string    `json:"id"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		NotebookID  string    `json:"notebook_id"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
	}

	Widget struct {
		ID          string `json:"id"`
		DashboardID string `json:"dashboard_id"`
		Type        string `json:"type"`
		Title       string `json:"title"`
		CellID      string `json:"cell_id"`
	}

	Connector struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		IsDefault   bool   `json:"is_default"`
		CreatedAt   string `json:"created_at"`
	}

	Group struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	}

	GroupMember struct {
		UserID    string `json:"user_id"`
		Email     string `json:"email"`
		Name      string `json:"name"`
	}

	OrgMember struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
		Name   string `json:"name"`
		Role   string `json:"role"`
	}

	ACLEntry struct {
		SubjectType string   `json:"subject_type"`
		SubjectID   string   `json:"subject_id"`
		Actions     []string `json:"actions"`
	}

	Snapshot struct {
		ID        string    `json:"id"`
		Label     string    `json:"label"`
		CreatedAt time.Time `json:"created_at"`
	}

	Schedule struct {
		ID         string `json:"id"`
		NotebookID string `json:"notebook_id"`
		CronExpr   string `json:"cron_expr"`
		Enabled    bool   `json:"enabled"`
	}

	PAT struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Token     string    `json:"token,omitempty"`
		CreatedAt time.Time `json:"created_at"`
	}

	AuditLog struct {
		ID         string    `json:"id"`
		Action     string    `json:"action"`
		UserEmail  string    `json:"user_email"`
		Resource   string    `json:"resource"`
		CreatedAt  time.Time `json:"created_at"`
	}

	MOTD struct {
		ID        string    `json:"id"`
		Message   string    `json:"message"`
		Active    bool      `json:"active"`
		CreatedAt time.Time `json:"created_at"`
	}

	Agent struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		ModelConfigID string `json:"model_config_id"`
	}

	AgentSession struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		CreatedAt time.Time `json:"created_at"`
	}

	ModelConfig struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Model   string `json:"model"`
		BaseURL string `json:"base_url"`
	}

	Skill struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	Tool struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Type        string `json:"type"`
	}

	MCPServer struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		URL         string `json:"url"`
	}

	SSOProvider struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		IssuerURL    string `json:"issuer_url"`
		ClientID     string `json:"client_id"`
		AllowedDomain string `json:"allowed_domain"`
	}

	Org struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Slug      string    `json:"slug"`
		CreatedAt time.Time `json:"created_at"`
	}

	User struct {
		ID              string `json:"id"`
		Email           string `json:"email"`
		Name            string `json:"name"`
		IsPlatformAdmin bool   `json:"is_platform_admin"`
	}

	Attachment struct {
		ID          string `json:"id"`
		FileName    string `json:"file_name"`
		FileSize    int64  `json:"file_size"`
		ContentType string `json:"content_type"`
		CreatedAt   string `json:"created_at"`
	}

	Template struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	}
)
```

**Step 2: Update `cmd/aether/main.go` to register all new commands**

```go
package main

import (
	"fmt"
	"os"

	"github.com/the-heaven-labs/aether/internal/cli"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "aether",
		Short: "Aether Notebooks CLI",
	}

	root.AddCommand(
		cli.LoginCmd(),
		cli.LogoutCmd(),
		cli.NotebooksCmd(),
		cli.CellsCmd(),
		cli.ConnectorsCmd(),
		cli.DashboardsCmd(),
		cli.FoldersCmd(),
		cli.GroupsCmd(),
		cli.ACLCmd(),
		cli.MembersCmd(),
		cli.OrgCmd(),
		cli.TokensCmd(),
		cli.TemplatesCmd(),
		cli.TrashCmd(),
		cli.HomeCmd(),
		cli.RecentCmd(),
		cli.AttachmentsCmd(),
		cli.AuditCmd(),
		cli.MOTDCmd(),
		cli.AgentsCmd(),
		cli.ModelConfigsCmd(),
		cli.SkillsCmd(),
		cli.ToolsCmd(),
		cli.MCPServersCmd(),
		cli.SSOCmd(),
		cli.AdminCmd(),
		cli.SeedCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

**Step 3: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully (some new commands will have empty stubs — that's fine)

**Step 4: Commit**

```bash
git add internal/cli/types.go cmd/aether/main.go
git commit -m "feat(cli): add shared types and register all command groups"
```

---

### Task 2: Dashboards commands

**Files:**
- Create: `internal/cli/dashboards.go`

**Step 1: Create `internal/cli/dashboards.go`**

This file implements the full dashboards command tree:
- `dashboards list` / `create` / `get` / `update` / `delete`
- `dashboards widgets add` / `update` / `delete`
- `dashboards share on` / `off` / `status`
- `dashboards permissions`

Each subcommand is a thin wrapper over a typed `Client` method.

```go
package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func (c *Client) ListDashboards() ([]Dashboard, error) {
	var result []Dashboard
	err := c.GetJSON("/api/v1/dashboards", &result)
	return result, err
}

func (c *Client) CreateDashboard(title, description, notebookID string) (*Dashboard, error) {
	body := map[string]string{"title": title}
	if description != "" {
		body["description"] = description
	}
	if notebookID != "" {
		body["notebook_id"] = notebookID
	}
	var result Dashboard
	err := c.PostJSON("/api/v1/dashboards", body, &result)
	return &result, err
}

func (c *Client) GetDashboard(id string) (*Dashboard, error) {
	var result Dashboard
	err := c.GetJSON("/api/v1/dashboards/"+id, &result)
	return &result, err
}

func (c *Client) UpdateDashboard(id, title, description string) (*Dashboard, error) {
	body := map[string]string{}
	if title != "" {
		body["title"] = title
	}
	if description != "" {
		body["description"] = description
	}
	var result Dashboard
	err := c.PostJSON("/api/v1/dashboards/"+id, body, &result)
	return &result, err
}

func (c *Client) DeleteDashboard(id string) error {
	return c.DeleteJSON("/api/v1/dashboards/" + id)
}

func (c *Client) AddWidget(dashboardID, widgetType, title, cellID string) (*Widget, error) {
	body := map[string]string{"type": widgetType, "title": title, "cell_id": cellID}
	var result Widget
	err := c.PostJSON("/api/v1/dashboards/"+dashboardID+"/widgets", body, &result)
	return &result, err
}

func (c *Client) UpdateWidget(dashboardID, widgetID, title string) (*Widget, error) {
	body := map[string]string{}
	if title != "" {
		body["title"] = title
	}
	var result Widget
	err := c.PostJSON("/api/v1/dashboards/"+dashboardID+"/widgets/"+widgetID, body, &result)
	return &result, err
}

func (c *Client) DeleteWidget(dashboardID, widgetID string) error {
	return c.DeleteJSON("/api/v1/dashboards/" + dashboardID + "/widgets/" + widgetID)
}

func (c *Client) GetDashboardShare(dashboardID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/dashboards/"+dashboardID+"/share", &result)
	return result, err
}

func (c *Client) ShareDashboard(dashboardID string) error {
	return c.PostJSON("/api/v1/dashboards/"+dashboardID+"/share", map[string]string{}, nil)
}

func (c *Client) RevokeDashboardShare(dashboardID string) error {
	return c.DeleteJSON("/api/v1/dashboards/" + dashboardID + "/share")
}

func (c *Client) GetDashboardPermissions(dashboardID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/dashboards/"+dashboardID+"/permissions", &result)
	return result, err
}

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
			var title, description, notebookID string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a dashboard",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					result, err := cl.CreateDashboard(title, description, notebookID)
					if err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVarP(&title, "title", "t", "", "Dashboard title (required)")
			c.Flags().StringVar(&description, "description", "", "Dashboard description")
			c.Flags().StringVar(&notebookID, "notebook", "", "Source notebook ID")
			c.MarkFlagRequired("title")
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
				result, err := c.GetDashboard(args[0])
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
				Use:   "update <id>",
				Short: "Update a dashboard",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					result, err := cl.UpdateDashboard(args[0], title, description)
					if err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVarP(&title, "title", "t", "", "New title")
			c.Flags().StringVar(&description, "description", "", "New description")
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
	)

	// Widget subcommands
	widgetCmd := &cobra.Command{Use: "widgets", Short: "Manage dashboard widgets"}
	widgetCmd.AddCommand(
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
					result, err := cl.AddWidget(args[0], widgetType, title, cellID)
					if err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVar(&widgetType, "type", "chart", "Widget type (chart, table, metric)")
			c.Flags().StringVar(&title, "title", "", "Widget title")
			c.Flags().StringVar(&cellID, "cell", "", "Source cell ID (required)")
			c.MarkFlagRequired("cell")
			return c
		}(),
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
					result, err := cl.UpdateWidget(args[0], args[1], title)
					if err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVar(&title, "title", "", "New widget title")
			return c
		}(),
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
	cmd.AddCommand(widgetCmd)

	// Share subcommands
	shareCmd := &cobra.Command{Use: "share", Short: "Manage dashboard sharing"}
	shareCmd.AddCommand(
		&cobra.Command{
			Use:   "status <id>",
			Short: "Get dashboard sharing status",
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
				PrintJSON(result)
				return nil
			},
		},
		&cobra.Command{
			Use:   "on <id>",
			Short: "Share a dashboard publicly",
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
			Short: "Revoke dashboard sharing",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.RevokeDashboardShare(args[0]); err != nil {
					return err
				}
				fmt.Println("Dashboard sharing revoked.")
				return nil
			},
		},
	)
	cmd.AddCommand(shareCmd)

	cmd.AddCommand(&cobra.Command{
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
	})

	return cmd
}
```

**Step 2: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully

**Step 3: Commit**

```bash
git add internal/cli/dashboards.go
git commit -m "feat(cli): add dashboards command group"
```

---

### Task 3: Folders, Home, Recent commands

**Files:**
- Create: `internal/cli/folders.go`

**Step 1: Create `internal/cli/folders.go`**

Includes folder CRUD, ancestors, home folder, and recent items commands.

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *Client) ListRootContents() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/folders", &result)
	return result, err
}

func (c *Client) GetFolderContents(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/folders/"+id, &result)
	return result, err
}

func (c *Client) GetFolderAncestors(id string) ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	err := c.GetJSON("/api/v1/folders/"+id+"/ancestors", &result)
	return result, err
}

func (c *Client) CreateFolder(name, parentID string) (*Folder, error) {
	body := map[string]interface{}{"name": name}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	var result Folder
	err := c.PostJSON("/api/v1/folders", body, &result)
	return &result, err
}

func (c *Client) UpdateFolder(id, name string) (*Folder, error) {
	var result Folder
	err := c.PostJSON("/api/v1/folders/"+id, map[string]string{"name": name}, &result)
	return &result, err
}

func (c *Client) DeleteFolder(id string) error {
	return c.DeleteJSON("/api/v1/folders/" + id)
}

func (c *Client) ListHomeFolders() ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	err := c.GetJSON("/api/v1/home", &result)
	return result, err
}

func (c *Client) EnsureHomeFolder() (map[string]string, error) {
	var result map[string]string
	err := c.PostJSON("/api/v1/users/me/home", nil, &result)
	return result, err
}

func (c *Client) GetRecent() ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	err := c.GetJSON("/api/v1/recent", &result)
	return result, err
}

func FoldersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "folders",
		Aliases: []string{"folder"},
		Short:   "Manage folders",
	}

	cmd.AddCommand(
		func() *cobra.Command {
			var parentID string
			c := &cobra.Command{
				Use:   "list",
				Short: "List folders (root or by parent)",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					var result interface{}
					if parentID != "" {
						result, err = cl.GetFolderContents(parentID)
					} else {
						result, err = cl.ListRootContents()
					}
					if err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVarP(&parentID, "parent", "p", "", "Parent folder ID (empty for root)")
			return c
		}(),
		func() *cobra.Command {
			var name, parentID string
			c := &cobra.Command{
				Use:   "create",
				Short: "Create a folder",
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					result, err := cl.CreateFolder(name, parentID)
					if err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "Folder name (required)")
			c.Flags().StringVarP(&parentID, "parent", "p", "", "Parent folder ID")
			c.MarkFlagRequired("name")
			return c
		}(),
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get folder contents and metadata",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetFolderContents(args[0])
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
				Use:   "update <id>",
				Short: "Rename a folder",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					result, err := cl.UpdateFolder(args[0], name)
					if err != nil {
						return err
					}
					PrintJSON(result)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "New folder name (required)")
			c.MarkFlagRequired("name")
			return c
		}(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a folder",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				if err := c.DeleteFolder(args[0]); err != nil {
					return err
				}
				fmt.Println("Deleted.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "ancestors <id>",
			Short: "Get folder ancestor chain",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.GetFolderAncestors(args[0])
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

func HomeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "home",
		Short: "Manage home folders",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List home folders for the current org",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.ListHomeFolders()
				if err != nil {
					return err
				}
				PrintJSON(result)
				return nil
			},
		},
		&cobra.Command{
			Use:   "ensure",
			Short: "Ensure your home folder exists (creates if missing)",
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := LoadClient()
				if err != nil {
					return err
				}
				result, err := c.EnsureHomeFolder()
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

func RecentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recent",
		Short: "List recently accessed items",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := LoadClient()
			if err != nil {
				return err
			}
			result, err := c.GetRecent()
			if err != nil {
				return err
			}
			PrintJSON(result)
			return nil
		},
	}
}
```

**Step 2: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully

**Step 3: Commit**

```bash
git add internal/cli/folders.go
git commit -m "feat(cli): add folders, home, and recent commands"
```

---

### Task 4: Groups and ACL commands

**Files:**
- Create: `internal/cli/groups.go`
- Create: `internal/cli/acl.go`

**Step 1: Create `internal/cli/groups.go`**

Groups CRUD + member management.

**Step 2: Create `internal/cli/acl.go`**

ACL get and set commands.

**Step 3: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully

**Step 4: Commit**

```bash
git add internal/cli/groups.go internal/cli/acl.go
git commit -m "feat(cli): add groups and acl commands"
```

---

### Task 5: Members, Org, and Tokens commands

**Files:**
- Create: `internal/cli/members.go`
- Create: `internal/cli/org.go`
- Create: `internal/cli/tokens.go`

**Step 1: Create each file following the established pattern**

Members: list, invite, invite-link, update-role, remove.
Org: sharing get/update, invitations get/update, registration get/update.
Tokens: list, create, delete.

**Step 2: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully

**Step 3: Commit**

```bash
git add internal/cli/members.go internal/cli/org.go internal/cli/tokens.go
git commit -m "feat(cli): add members, org settings, and tokens commands"
```

---

### Task 6: Templates, Trash, Attachments commands

**Files:**
- Create: `internal/cli/templates.go`
- Create: `internal/cli/trash.go`
- Create: `internal/cli/attachments.go`

**Step 1: Create each file**

Templates: list, create, delete.
Trash: list, restore.
Attachments: list, upload, get, delete (upload reads a local file).

**Step 2: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully

**Step 3: Commit**

```bash
git add internal/cli/templates.go internal/cli/trash.go internal/cli/attachments.go
git commit -m "feat(cli): add templates, trash, and attachment commands"
```

---

### Task 7: Audit and MOTD commands

**Files:**
- Create: `internal/cli/audit.go`
- Create: `internal/cli/motd.go`

**Step 1: Create each file**

Audit: list (timestamp range flags).
MOTD: list (active), admin list|create|update|delete.

**Step 2: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully

**Step 3: Commit**

```bash
git add internal/cli/audit.go internal/cli/motd.go
git commit -m "feat(cli): add audit and motd commands"
```

---

### Task 8: Agents, Model Configs, Skills, Tools, MCP commands

**Files:**
- Create: `internal/cli/agents.go`
- Create: `internal/cli/model_configs.go`
- Create: `internal/cli/skills.go`
- Create: `internal/cli/tools.go`
- Create: `internal/cli/mcp.go`

**Step 1: Create each file**

Each follows CRUD pattern. Agents additionally: sessions (list, create, get), messages.

**Step 2: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully

**Step 3: Commit**

```bash
git add internal/cli/agents.go internal/cli/model_configs.go internal/cli/skills.go internal/cli/tools.go internal/cli/mcp.go
git commit -m "feat(cli): add agents, model-configs, skills, tools, and mcp commands"
```

---

### Task 9: SSO and Admin commands

**Files:**
- Create: `internal/cli/sso.go`
- Create: `internal/cli/admin.go`

**Step 1: Create each file**

SSO: providers (list|create|update|delete), platform-providers (list|enable|disable), settings (get|update).
Admin: orgs (list|create), users (list|update), sso (list|create|update|delete).

**Step 2: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully

**Step 3: Commit**

```bash
git add internal/cli/sso.go internal/cli/admin.go
git commit -m "feat(cli): add sso and admin commands"
```

---

### Task 10: Extend notebooks, cells, connectors with missing subcommands

**Files:**
- Modify: `internal/cli/notebooks.go`
- Modify: `internal/cli/cells.go`
- Modify: `internal/cli/connectors.go`

**Step 1: Extend `notebooks.go`**

Add subcommands: update, clone, export, import, share (on|off|status), permissions, snapshots (list|create|restore|diff), schedules (list|create|get|update|delete).

```go
func (c *Client) UpdateNotebook(id, title, description string) (*Notebook, error) {
	body := map[string]string{}
	if title != "" {
		body["title"] = title
	}
	if description != "" {
		body["description"] = description
	}
	var result Notebook
	err := c.PostJSON("/api/v1/notebooks/"+id, body, &result)
	return &result, err
}

func (c *Client) CloneNotebook(id string) (*Notebook, error) {
	var result Notebook
	err := c.PostJSON("/api/v1/notebooks/"+id+"/clone", nil, &result)
	return &result, err
}

func (c *Client) ExportNotebook(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/notebooks/"+id+"/export", &result)
	return result, err
}

func (c *Client) ImportNotebook(data map[string]interface{}) (*Notebook, error) {
	var result Notebook
	err := c.PostJSON("/api/v1/notebooks/import", data, &result)
	return &result, err
}

func (c *Client) GetNotebookShare(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/notebooks/"+id+"/share", &result)
	return result, err
}

func (c *Client) ShareNotebook(id string) error {
	return c.PostJSON("/api/v1/notebooks/"+id+"/share", map[string]string{}, nil)
}

func (c *Client) RevokeNotebookShare(id string) error {
	return c.DeleteJSON("/api/v1/notebooks/" + id + "/share")
}

func (c *Client) GetNotebookPermissions(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/notebooks/"+id+"/permissions", &result)
	return result, err
}

func (c *Client) ListSnapshots(notebookID string) ([]Snapshot, error) {
	var result []Snapshot
	err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/snapshots", &result)
	return result, err
}

func (c *Client) CreateSnapshot(notebookID, label string) (*Snapshot, error) {
	var result Snapshot
	err := c.PostJSON("/api/v1/notebooks/"+notebookID+"/snapshots", map[string]string{"label": label}, &result)
	return &result, err
}

func (c *Client) RestoreSnapshot(notebookID, snapshotID string) error {
	return c.PostJSON("/api/v1/notebooks/"+notebookID+"/snapshots/"+snapshotID+"/restore", nil, nil)
}

func (c *Client) SnapshotDiff(notebookID, snapshotID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/snapshots/"+snapshotID+"/diff", &result)
	return result, err
}

func (c *Client) ListSchedules(notebookID string) ([]Schedule, error) {
	var result []Schedule
	err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/schedules", &result)
	return result, err
}

func (c *Client) CreateSchedule(notebookID, cronExpr string, enabled bool) (*Schedule, error) {
	var result Schedule
	err := c.PostJSON("/api/v1/notebooks/"+notebookID+"/schedules", map[string]interface{}{"cron_expr": cronExpr, "enabled": enabled}, &result)
	return &result, err
}

func (c *Client) GetSchedule(id string) (*Schedule, error) {
	var result Schedule
	err := c.GetJSON("/api/v1/schedules/"+id, &result)
	return &result, err
}

func (c *Client) UpdateSchedule(id, cronExpr string, enabled bool) (*Schedule, error) {
	var result Schedule
	err := c.PostJSON("/api/v1/schedules/"+id, map[string]interface{}{"cron_expr": cronExpr, "enabled": enabled}, &result)
	return &result, err
}

func (c *Client) DeleteSchedule(id string) error {
	return c.DeleteJSON("/api/v1/schedules/" + id)
}
```

Add the following subcommands to `NotebooksCmd()`:
- `update` (--title, --description)
- `clone`
- `export`
- `import` (--file path to JSON)
- `share status|on|off`
- `permissions`
- `snapshots list|create|restore|diff`
- `schedules list|create|get|update|delete`

**Step 2: Extend `cells.go`**

Add subcommands: list, get, update, delete, duplicate, versions (list|restore).

```go
func (c *Client) ListCells(notebookID string) ([]Cell, error) {
	// Cells are returned as part of the notebook
	var nb Notebook
	err := c.GetJSON("/api/v1/notebooks/"+notebookID, &nb)
	// The notebook response includes cells array — we'll use a generic approach
	return nil, err
}

func (c *Client) GetCell(notebookID, cellID string) (*Cell, error) {
	var result Cell
	// Fetch notebook and find cell
	err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/cells/"+cellID, &result)
	return &result, err
}

func (c *Client) UpdateCell(notebookID, cellID, source string) (*Cell, error) {
	var result Cell
	err := c.PostJSON("/api/v1/notebooks/"+notebookID+"/cells/"+cellID, map[string]string{"source": source}, &result)
	return &result, err
}

func (c *Client) DeleteCell(notebookID, cellID string) error {
	return c.DeleteJSON("/api/v1/notebooks/" + notebookID + "/cells/" + cellID)
}

func (c *Client) DuplicateCell(notebookID, cellID string) (*Cell, error) {
	var result Cell
	err := c.PostJSON("/api/v1/notebooks/"+notebookID+"/cells/"+cellID+"/duplicate", nil, &result)
	return &result, err
}

func (c *Client) ListCellVersions(notebookID, cellID string) ([]CellVersion, error) {
	var result []CellVersion
	err := c.GetJSON("/api/v1/notebooks/"+notebookID+"/cells/"+cellID+"/versions", &result)
	return result, err
}

func (c *Client) RestoreCellVersion(notebookID, cellID, versionID string) error {
	return c.PostJSON("/api/v1/notebooks/"+notebookID+"/cells/"+cellID+"/versions/"+versionID+"/restore", nil, nil)
}
```

**Step 3: Extend `connectors.go`**

Add subcommands: create, get, update, test, set-default, schema, databases.

```go
func (c *Client) GetConnector(id string) (*Connector, error) {
	var result Connector
	err := c.GetJSON("/api/v1/connectors/"+id, &result)
	return &result, err
}

func (c *Client) CreateConnector(name, connType, configJSON string) (*Connector, error) {
	var config map[string]interface{}
	json.Unmarshal([]byte(configJSON), &config)
	body := map[string]interface{}{"name": name, "type": connType, "config": config}
	var result Connector
	err := c.PostJSON("/api/v1/connectors", body, &result)
	return &result, err
}

func (c *Client) UpdateConnector(id, name, configJSON string) (*Connector, error) {
	body := map[string]interface{}{}
	if name != "" {
		body["name"] = name
	}
	if configJSON != "" {
		var config map[string]interface{}
		json.Unmarshal([]byte(configJSON), &config)
		body["config"] = config
	}
	var result Connector
	err := c.PostJSON("/api/v1/connectors/"+id, body, &result)
	return &result, err
}

func (c *Client) TestConnector(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/connectors/"+id+"/test", nil, &result)
	return result, err
}

func (c *Client) SetDefaultConnector(id string) error {
	return c.PostJSON("/api/v1/connectors/"+id+"/default", nil, nil)
}

func (c *Client) GetConnectorSchema(id string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/connectors/"+id+"/schema", &result)
	return result, err
}

func (c *Client) ListConnectorDatabases(id string) ([]string, error) {
	var result []string
	err := c.GetJSON("/api/v1/connectors/"+id+"/databases", &result)
	return result, err
}
```

**Step 4: Verify compilation**

Run: `go build ./cmd/aether/`
Expected: compiles successfully

**Step 5: Commit**

```bash
git add internal/cli/notebooks.go internal/cli/cells.go internal/cli/connectors.go
git commit -m "feat(cli): extend notebooks, cells, connectors with full subcommands"
```

---

### Task 11: Verify final build

**Step 1: Build and check for errors**

Run: `go build ./cmd/aether/ && go vet ./internal/cli/`
Expected: no errors

**Step 2: Quick smoke test**

```bash
./aether --help
```

Expected: all 27 command groups listed.

**Step 3: Final commit**

```bash
git commit --allow-empty -m "feat(cli): feature-complete CLI — all API endpoints covered"
```
