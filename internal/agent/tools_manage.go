package agent

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
	"github.com/the-heaven-labs/aether/internal/models"
	"github.com/the-heaven-labs/aether/internal/validate"
)

func RegisterManageTools(reg *ToolRegistry, pool *pgxpool.Pool) {
	// Dashboard tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_dashboard",
			Description: "Create a new dashboard. Returns the dashboard ID.",
			Parameters:  `{"type":"object","properties":{"title":{"type":"string"},"folder_id":{"type":"string","description":"Optional parent folder"}},"required":["title"]}`,
		},
		Handler: makeCreateDashboardHandler(pool),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_dashboards",
			Description: "List all dashboards in the organization. Returns id, title, and timestamps.",
			Parameters:  `{"type":"object","properties":{}}`,
		},
		Handler: makeListDashboardsHandler(pool),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "delete_dashboard",
			Description: "Delete a dashboard and all its widgets.",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"}},"required":["dashboard_id"]}`,
		},
		Handler: makeDeleteDashboardHandler(pool),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_dashboard_widget",
			Description: "Add a widget to a dashboard. A widget reads either a notebook cell (notebook_id + cell_id) or a query (connector_id + query); config carries per-widget options (e.g. config.metric for metric widgets).",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"},"notebook_id":{"type":"string","description":"Notebook for a cell-backed widget"},"cell_id":{"type":"string","description":"Cell for a cell-backed widget"},"connector_id":{"type":"string","description":"Connector for a query-backed widget (requires query)"},"query":{"type":"string","description":"SQL for a query-backed widget (requires connector_id)"},"type":{"type":"string","enum":["chart","table","metric","text"],"description":"Widget type — use 'chart' for cells with chart config, 'table' for table output, 'metric' for a single value, 'text' for markdown"},"config":{"type":"object","description":"Per-widget config, e.g. {\"metric\":{\"value_column\":\"x\",\"aggregation\":\"sum\"}}"},"row":{"type":"number","description":"Row position (0-based)"},"col":{"type":"number","description":"Column position (0-based)"},"width":{"type":"number","description":"Widget width in columns (default 6)"},"height":{"type":"number","description":"Widget height in rows (default 4)"}},"required":["dashboard_id","type"]}`,
		},
		Handler: makeCreateDashboardWidgetHandler(pool),
		Timeout: 15 * time.Second,
	})

	// Schedule tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_schedule",
			Description: "Schedule a notebook for recurring execution using a cron expression.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"},"cron_expression":{"type":"string","description":"Cron expression like '0 9 * * 1-5' (every weekday at 9am)"}},"required":["notebook_id","cron_expression"]}`,
		},
		Handler: makeCreateScheduleHandler(pool),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_dashboard_widget",
			Description: "Update a dashboard widget: position (row, col), size (width, height), type (chart/table/metric/text), tab (App chapter), or config keys to shallow-merge into the widget's config.",
			Parameters:  `{"type":"object","properties":{"widget_id":{"type":"string"},"dashboard_id":{"type":"string"},"row":{"type":"number"},"col":{"type":"number"},"width":{"type":"number"},"height":{"type":"number"},"tab":{"type":"string","description":"App tab id (lowercase letters, digits, dashes); empty falls back to the first tab"},"type":{"type":"string","enum":["chart","table","metric","text"]},"config":{"type":"object","description":"Config keys to shallow-merge into the widget's existing config"}},"required":["widget_id","dashboard_id"]}`,
		},
		Handler: makeUpdateDashboardWidgetHandler(pool),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "get_dashboard",
			Description: "Get a dashboard with its widgets. Use this to see the current state of a dashboard before making changes.",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"}},"required":["dashboard_id"]}`,
		},
		Handler: makeGetDashboardHandler(pool),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "delete_dashboard_widget",
			Description: "Remove a widget from a dashboard.",
			Parameters:  `{"type":"object","properties":{"widget_id":{"type":"string"},"dashboard_id":{"type":"string"}},"required":["widget_id","dashboard_id"]}`,
		},
		Handler: makeDeleteDashboardWidgetHandler(),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_dashboard",
			Description: "Update a dashboard's title or grid settings (e.g., grid_cols: 12 for 12-column grid).",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"},"title":{"type":"string"},"grid_cols":{"type":"number","description":"Number of columns in the grid layout (default 12)"}},"required":["dashboard_id"]}`,
		},
		Handler: makeUpdateDashboardHandler(),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_dashboard_settings",
			Description: "Update a dashboard's App settings: title, app (e.g. app.title, app.subtitle, app.theme with hex colors), and tabs (App chapters, e.g. [{id, title}]).",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"},"title":{"type":"string","description":"Dashboard title"},"app":{"type":"object","description":"App surface settings; filter_scope is \"all\" (default) or \"tab\" (each chapter shows only the filters its widgets use); theme fields are hex colors, e.g. {\"title\":\"Sales\",\"filter_scope\":\"tab\",\"theme\":{\"accent\":\"#7c5cff\",\"color_scheme\":\"dark\"}}"},"tabs":{"type":"array","description":"App chapters; ids are lowercase letters/digits/dashes","items":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"}},"required":["id","title"]}}},"required":["dashboard_id"]}`,
		},
		Handler: makeUpdateDashboardSettingsHandler(pool),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "set_dashboard_layout",
			Description: "Arrange a dashboard's App surface: set tabs, place widgets into tabs (placements), and merge widget appearance (e.g. {background: transparent|card, padding: none|sm|md, align: left|center|right, border}).",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"},"tabs":{"type":"array","description":"Replaces the App tab list","items":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"}},"required":["id","title"]}},"placements":{"type":"array","description":"Move widgets into tabs","items":{"type":"object","properties":{"widget_id":{"type":"string"},"tab":{"type":"string","description":"Tab id; empty falls back to the first tab"}},"required":["widget_id","tab"]}},"appearance":{"type":"array","description":"Merge appearance into each widget's config","items":{"type":"object","properties":{"widget_id":{"type":"string"},"appearance":{"type":"object"}},"required":["widget_id","appearance"]}}},"required":["dashboard_id"]}`,
		},
		Handler: makeSetDashboardLayoutHandler(),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "publish_dashboard",
			Description: "Publish the dashboard's current draft as a new version. Viewers see the latest published version until the next publish.",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"}},"required":["dashboard_id"]}`,
		},
		Handler: makePublishDashboardHandler(pool),
		Timeout: 30 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "share_dashboard",
			Description: "Generate a public shareable link for a dashboard. Returns the public token and the full URL.",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"}},"required":["dashboard_id"]}`,
		},
		Handler: makeShareDashboardHandler(pool),
		Timeout: 15 * time.Second,
	})

	// Schedule tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "delete_schedule",
			Description: "Remove a notebook schedule.",
			Parameters:  `{"type":"object","properties":{"schedule_id":{"type":"string"}},"required":["schedule_id"]}`,
		},
		Handler: makeDeleteScheduleHandler(pool),
		Timeout: 15 * time.Second,
	})

	// Permission tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "read_permissions",
			Description: "List ACL entries for a resource (notebook, dashboard, connector, folder). Returns user, group, and everyone entries with their actions.",
			Parameters:  `{"type":"object","properties":{"resource_type":{"type":"string","enum":["notebook","dashboard","connector","folder"]},"resource_id":{"type":"string"}},"required":["resource_type","resource_id"]}`,
		},
		Handler: makeReadPermissionsHandler(pool),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_permissions",
			Description: "Set ACL entries for a resource. Replaces all existing entries. Each entry has subject_type (user/group/everyone), subject_id (user or group ID, omit for everyone), and actions (array like ['view','edit','run','delete']).",
			Parameters:  `{"type":"object","properties":{"resource_type":{"type":"string","enum":["notebook","dashboard","connector","folder"]},"resource_id":{"type":"string"},"entries":{"type":"array","items":{"type":"object","properties":{"subject_type":{"type":"string","enum":["user","group","everyone"]},"subject_id":{"type":"string","description":"User or group ID. Omit for 'everyone'."},"actions":{"type":"array","items":{"type":"string"}}}}}},"required":["resource_type","resource_id","entries"]}`,
		},
		Handler: makeUpdatePermissionsHandler(pool),
		Timeout: 30 * time.Second,
	})

	// Export / Import tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "export_notebook",
			Description: "Export a notebook as Jupyter .ipynb format. Returns the JSON content that can be downloaded as .ipynb.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"}},"required":["notebook_id"]}`,
		},
		Handler: makeExportNotebookHandler(pool),
		Timeout: 60 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "import_notebook",
			Description: "Import a notebook from Jupyter .ipynb JSON content. Creates a new notebook with all cells.",
			Parameters:  `{"type":"object","properties":{"title":{"type":"string","description":"Title for the imported notebook"},"ipynb_json":{"type":"string","description":"The full .ipynb JSON string"},"folder_id":{"type":"string","description":"Optional parent folder ID"}},"required":["title","ipynb_json"]}`,
		},
		Handler: makeImportNotebookHandler(pool),
		Timeout: 120 * time.Second,
	})
}

func makeCreateDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Title    string  `json:"title"`
			FolderID *string `json:"folder_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.Title == "" {
			return nil, fmt.Errorf("title is required")
		}
		if req.FolderID != nil && *req.FolderID != "" {
			if err := ctx.CheckPermission("folder", *req.FolderID, "edit"); err != nil {
				return nil, err
			}
		}

		id := uuid.New().String()
		now := time.Now()
		_, err := pool.Exec(ctx.Context, `
			INSERT INTO dashboards (id, org_id, title, settings, created_by, folder_id, created_at, updated_at)
			VALUES ($1, $2, $3, '{}', $4, $5, $6, $6)
		`, id, ctx.OrgID, req.Title, ctx.UserID, req.FolderID, now)
		if err != nil {
			return nil, fmt.Errorf("create dashboard: %w", err)
		}

		_, aclErr := pool.Exec(ctx.Context, `
			INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			VALUES ($1, 'dashboard', $2::uuid, 'user', $3, ARRAY['view','edit','delete','share'])
			ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
			ctx.OrgID, id, ctx.UserID,
		)
		if aclErr != nil {
			slog.Warn("seed ACL entries for dashboard", "dashboard_id", id, "error", aclErr)
		}

		_ = ctx.AuditLog("dashboard.create", "dashboard", id)
		return map[string]any{"dashboard_id": id, "title": req.Title}, nil
	}
}

// validDashboardWidgetType reports whether t is a widget type the dashboard
// document supports (mirroring the widgets.type CHECK constraint). Validating
// before the document write keeps a rejected request from lazily seeding the
// dashboard document.
func validDashboardWidgetType(t string) bool {
	switch models.WidgetType(t) {
	case models.WidgetChart, models.WidgetTable, models.WidgetText, models.WidgetMetric:
		return true
	default:
		return false
	}
}

// validateDashboardWidgetCellRef rejects a notebook/cell pair that does not
// resolve to a real cell in that notebook. The document store accepts the
// pair, but the materializer skips the widget (deleting its derived row), so
// without this check a success response would commit a phantom widget that
// never materializes. Mirrors the REST validateWidgetCellRef fix; shared by
// create (the requested pair) and update (the merged widget's pair).
func validateDashboardWidgetCellRef(ctx context.Context, pool *pgxpool.Pool, notebookID, cellID string) error {
	if _, err := uuid.Parse(notebookID); err != nil {
		return fmt.Errorf("cell not found")
	}
	if _, err := uuid.Parse(cellID); err != nil {
		return fmt.Errorf("cell not found")
	}
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM cells WHERE id = $1 AND notebook_id = $2)`,
		cellID, notebookID).Scan(&exists); err != nil {
		return fmt.Errorf("check cell reference: %w", err)
	}
	if !exists {
		return fmt.Errorf("cell not found")
	}
	return nil
}

// validateDashboardWidgetConnectorRef rejects a query widget whose connector
// reference is missing, soft-deleted, or outside the org. The document store
// does not check connector existence, and the materializer skips widgets with
// dangling references (deleting their derived rows), so without this check a
// success response would silently drop the widget. Mirrors the REST
// validateWidgetConnectorRef.
func validateDashboardWidgetConnectorRef(ctx context.Context, pool *pgxpool.Pool, orgID, connectorID string) error {
	if _, err := uuid.Parse(connectorID); err != nil {
		return fmt.Errorf("connector not found")
	}
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM connectors WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL)`,
		connectorID, orgID).Scan(&exists); err != nil {
		return fmt.Errorf("check connector reference: %w", err)
	}
	if !exists {
		return fmt.Errorf("connector not found")
	}
	return nil
}

// validateDashboardLogoAttachment verifies attachmentID names an org-scoped,
// non-notebook attachment in orgID, mirroring the REST validateLogoAttachment:
// rejecting unknown ids at write time keeps app surfaces from rendering broken
// images, and notebook attachments are rejected too because a logo is served
// through the public attachment route, which has no notebook view ACL. An
// empty id is valid (no logo) and a malformed id can never match, so it is
// reported as not found.
func validateDashboardLogoAttachment(ctx context.Context, pool *pgxpool.Pool, orgID, attachmentID string) error {
	if attachmentID == "" {
		return nil
	}
	// Mirror the API's isValidUUID: only the canonical lowercase spelling is
	// accepted, so an id that the public attachment route would reject can
	// never be stored as a logo.
	if u, err := uuid.Parse(attachmentID); err != nil || u.String() != attachmentID {
		return fmt.Errorf("logo attachment not found")
	}
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM attachments WHERE id = $1 AND org_id = $2 AND notebook_id IS NULL)`,
		attachmentID, orgID).Scan(&exists); err != nil {
		return fmt.Errorf("logo attachment lookup: %w", err)
	}
	if !exists {
		return fmt.Errorf("logo attachment not found")
	}
	return nil
}

func makeCreateDashboardWidgetHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string         `json:"dashboard_id"`
			NotebookID  string         `json:"notebook_id"`
			CellID      string         `json:"cell_id"`
			ConnectorID string         `json:"connector_id"`
			Query       string         `json:"query"`
			Type        string         `json:"type"`
			Config      map[string]any `json:"config"`
			Row         int            `json:"row"`
			Col         int            `json:"col"`
			Width       int            `json:"width"`
			Height      int            `json:"height"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.Width == 0 {
			req.Width = 6
		}
		if req.Height == 0 {
			req.Height = 4
		}

		// Check permission on the dashboard
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}

		// Exactly one widget source: a query (connector + SQL) or a notebook
		// cell. The REST path also allows source-less widgets for backwards
		// compatibility; the agent tool deliberately does not, so a widget it
		// creates always has something to render.
		hasQuery := req.ConnectorID != "" || req.Query != ""
		hasCell := req.NotebookID != "" || req.CellID != ""
		if hasQuery && hasCell {
			return nil, fmt.Errorf("widget cannot reference both a notebook cell and a query connector")
		}
		if !hasQuery && !hasCell {
			return nil, fmt.Errorf("widget requires either connector_id and query, or notebook_id and cell_id")
		}
		if hasQuery {
			if req.ConnectorID == "" {
				return nil, fmt.Errorf("connector_id is required for query widgets")
			}
			if req.Query == "" {
				return nil, fmt.Errorf("query is required for query widgets")
			}
		} else if req.NotebookID == "" || req.CellID == "" {
			return nil, fmt.Errorf("cell widgets require notebook_id and cell_id")
		}

		if ctx.DashboardDocStore == nil {
			return nil, fmt.Errorf("dashboard document store not configured")
		}
		if !validDashboardWidgetType(req.Type) {
			return nil, fmt.Errorf("invalid widget type")
		}

		if hasCell {
			// Validate the cell reference's notebook is viewable (prevents
			// IDOR escalation) before checking the pair resolves.
			if err := ctx.CheckPermission("notebook", req.NotebookID, "view"); err != nil {
				return nil, err
			}
			// A dangling cell reference must fail the request: the document
			// store accepts the pair, but the materializer skips the widget
			// (deleting its derived row), so a success response would commit a
			// phantom widget that never materializes.
			if err := validateDashboardWidgetCellRef(ctx.Context, pool, req.NotebookID, req.CellID); err != nil {
				return nil, err
			}
		} else {
			// A dangling connector reference must fail the request for the
			// same reason.
			if err := validateDashboardWidgetConnectorRef(ctx.Context, pool, ctx.OrgID, req.ConnectorID); err != nil {
				return nil, err
			}
		}

		// Validate layout bounds and overlap against the materialized rows
		// (kept current by every document store).
		if err := validate.WidgetLayout(ctx.Context, pool, req.DashboardID, models.WidgetLayout{
			Row:    req.Row,
			Col:    req.Col,
			Width:  req.Width,
			Height: req.Height,
		}, ""); err != nil {
			return nil, fmt.Errorf("invalid layout: %w", err)
		}

		// Chart widgets render the notebook cell's metadata.chart; widgets.config
		// holds only explicit per-widget overrides flagged with
		// config_edited_from_dashboard. Never snapshot the cell config here — a
		// creation-time copy would shadow later notebook edits. Query widgets
		// take the caller's config as-is (e.g. config.metric for metric
		// widgets).
		widgetConfig := req.Config
		if widgetConfig == nil {
			widgetConfig = map[string]any{}
		}

		// The widget is written through the dashboard document (the source of
		// truth); Postgres is a derived read cache materialized by the store.
		id := uuid.NewString()
		widget := dashboarddoc.WidgetDoc{
			ID:       id,
			Type:     req.Type,
			Layout:   dashboarddoc.Layout{Row: req.Row, Col: req.Col, Width: req.Width, Height: req.Height},
			Language: "sql",
			Config:   widgetConfig,
		}
		if hasQuery {
			widget.ConnectorID = &req.ConnectorID
			widget.Query = &req.Query
		} else {
			widget.NotebookID = &req.NotebookID
			widget.CellID = &req.CellID
		}

		state, err := ctx.DashboardDocStore.LoadOrSeed(ctx.Context, ctx.OrgID, req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("load dashboard document: %w", err)
		}
		newState, err := dashboarddoc.UpsertWidget(state, widget)
		if err != nil {
			return nil, fmt.Errorf("create widget: %w", err)
		}
		if err := ctx.DashboardDocStore.Store(ctx.Context, req.DashboardID, newState); err != nil {
			return nil, fmt.Errorf("create widget: %w", err)
		}

		_ = ctx.AuditLog("dashboard.add_widget", "dashboard", req.DashboardID)
		return map[string]any{"widget_id": id}, nil
	}
}

func makeUpdateDashboardWidgetHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			WidgetID    string         `json:"widget_id"`
			DashboardID string         `json:"dashboard_id"`
			Row         *int           `json:"row"`
			Col         *int           `json:"col"`
			Width       *int           `json:"width"`
			Height      *int           `json:"height"`
			Tab         *string        `json:"tab"`
			WidgetType  string         `json:"type"`
			Config      map[string]any `json:"config"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}
		if ctx.DashboardDocStore == nil {
			return nil, fmt.Errorf("dashboard document store not configured")
		}

		// A request with nothing updatable must not rewrite (or re-publish)
		// the widget: report it like update_dashboard's no-op guard instead.
		if req.Row == nil && req.Col == nil && req.Width == nil && req.Height == nil &&
			req.Tab == nil && req.WidgetType == "" && req.Config == nil {
			return nil, fmt.Errorf("nothing to update")
		}
		if req.WidgetType != "" && !validDashboardWidgetType(req.WidgetType) {
			return nil, fmt.Errorf("invalid widget type")
		}
		if req.Tab != nil {
			if err := validate.WidgetTab(*req.Tab); err != nil {
				return nil, err
			}
		}

		// Document widget keys are canonical lowercase UUIDs; canonicalize the
		// requested ID so any UUID spelling addresses the same widget, and a
		// non-UUID can never address one.
		widgetUUID, err := uuid.Parse(req.WidgetID)
		if err != nil {
			return nil, fmt.Errorf("widget not found")
		}
		widgetID := widgetUUID.String()

		// Read-merge: the request is partial, so the projected widget is the
		// base for validation, and only the provided fields are applied to the
		// document through field-level ops. Each op mutates one field of the
		// live widget map, so a layout-only update keeps query/connector/
		// notebook untouched even while an editor types SQL into the same
		// widget.
		state, err := ctx.DashboardDocStore.LoadOrSeed(ctx.Context, ctx.OrgID, req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("load dashboard document: %w", err)
		}
		proj, err := dashboarddoc.Project(state)
		if err != nil {
			return nil, fmt.Errorf("read dashboard document: %w", err)
		}
		existing, ok := proj.Widgets[widgetID]
		if !ok {
			return nil, fmt.Errorf("widget not found")
		}

		// Merged layout for validation only: the ops below write the same
		// values into the document.
		hasLayout := req.Row != nil || req.Col != nil || req.Width != nil || req.Height != nil || req.Tab != nil
		layout := existing.Layout
		if req.Row != nil {
			layout.Row = *req.Row
		}
		if req.Col != nil {
			layout.Col = *req.Col
		}
		if req.Width != nil {
			layout.Width = *req.Width
		}
		if req.Height != nil {
			layout.Height = *req.Height
		}
		if req.Tab != nil {
			layout.Tab = *req.Tab
		}

		// The request's config is a shallow patch over the projected config:
		// provided keys replace their counterparts and absent keys stay, so a
		// metric-only update keeps appearance/chart overrides (and vice versa).
		mergedConfig := existing.Config
		if req.Config != nil {
			mergedConfig = make(map[string]any, len(existing.Config)+len(req.Config))
			for k, v := range existing.Config {
				mergedConfig[k] = v
			}
			for k, v := range req.Config {
				mergedConfig[k] = v
			}
		}

		// A merged notebook/cell pair (the request has no ref fields, so it
		// comes from the existing widget) must still resolve to a real cell:
		// the materializer would otherwise skip the widget and delete its
		// derived row while the document keeps it.
		if existing.NotebookID != nil && existing.CellID != nil {
			if err := validateDashboardWidgetCellRef(ctx.Context, pool, *existing.NotebookID, *existing.CellID); err != nil {
				return nil, err
			}
		}

		// Validate layout bounds and overlap against the materialized rows
		// (kept current by every document store). The merged tab rides along
		// so the overlap check is chapter-aware.
		if hasLayout {
			if err := validate.WidgetLayout(ctx.Context, pool, req.DashboardID, models.WidgetLayout{
				Row:    layout.Row,
				Col:    layout.Col,
				Width:  layout.Width,
				Height: layout.Height,
				Tab:    layout.Tab,
			}, widgetID); err != nil {
				return nil, fmt.Errorf("invalid layout: %w", err)
			}
		}

		newState := state
		if hasLayout {
			newState, err = dashboarddoc.UpdateLayout(newState, widgetID, layout)
			if err != nil {
				return nil, fmt.Errorf("update widget: %w", err)
			}
		}
		if req.WidgetType != "" {
			newState, err = dashboarddoc.SetWidgetType(newState, widgetID, req.WidgetType)
			if err != nil {
				return nil, fmt.Errorf("update widget: %w", err)
			}
		}
		if req.Config != nil {
			newState, err = dashboarddoc.SetWidgetConfig(newState, widgetID, mergedConfig)
			if err != nil {
				return nil, fmt.Errorf("update widget: %w", err)
			}
		}
		if err := ctx.DashboardDocStore.Store(ctx.Context, req.DashboardID, newState); err != nil {
			return nil, fmt.Errorf("update widget: %w", err)
		}

		return map[string]any{"widget_id": widgetID}, nil
	}
}

func makeGetDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string `json:"dashboard_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "view"); err != nil {
			return nil, err
		}

		var title string
		var settingsJSON []byte
		err := pool.QueryRow(ctx.Context,
			`SELECT title, COALESCE(settings, '{}') FROM dashboards WHERE id = $1 AND org_id = $2`,
			req.DashboardID, ctx.OrgID,
		).Scan(&title, &settingsJSON)
		if err != nil {
			return nil, fmt.Errorf("dashboard not found: %w", err)
		}

		var settings map[string]any
		json.Unmarshal(settingsJSON, &settings)

		// Fetch widgets. Query-backed widgets have NULL notebook_id/cell_id
		// (and cell widgets NULL connector_id/query), so the references are
		// scanned as nullable pointers: a non-nullable scan fails on the first
		// query widget and aborts the whole iteration, hiding every widget.
		rows, err := pool.Query(ctx.Context,
			`SELECT id, notebook_id, cell_id, connector_id, query, language, type, layout, config
			 FROM widgets WHERE dashboard_id = $1 ORDER BY created_at ASC`,
			req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("load widgets: %w", err)
		}
		defer rows.Close()

		var widgets []map[string]any
		for rows.Next() {
			var id, language, wType string
			var nbID, cellID, connectorID, query *string
			var layoutJSON, configJSON []byte
			if err := rows.Scan(&id, &nbID, &cellID, &connectorID, &query, &language, &wType, &layoutJSON, &configJSON); err != nil {
				return nil, fmt.Errorf("scan widget: %w", err)
			}
			var layout map[string]any
			json.Unmarshal(layoutJSON, &layout)
			config := map[string]any{}
			if len(configJSON) > 0 {
				json.Unmarshal(configJSON, &config)
			}
			widgets = append(widgets, map[string]any{
				"id": id, "type": wType, "layout": layout, "language": language,
				"notebook_id": nbID, "cell_id": cellID,
				"connector_id": connectorID, "query": query,
				"config": config,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("load widgets iter: %w", err)
		}
		if widgets == nil {
			widgets = []map[string]any{}
		}

		// Org branding rides along so the agent can read the org's theme
		// tokens; a read failure degrades to null rather than failing the
		// whole read (mirrors the API's orgBranding helper).
		var orgBranding map[string]any
		var brandingJSON []byte
		if err := pool.QueryRow(ctx.Context,
			`SELECT COALESCE(settings->'branding', 'null'::jsonb) FROM orgs WHERE id = $1`, ctx.OrgID,
		).Scan(&brandingJSON); err != nil {
			slog.Debug("org branding read failed", "org_id", ctx.OrgID, "error", err)
		} else if err := json.Unmarshal(brandingJSON, &orgBranding); err != nil {
			slog.Debug("org branding decode failed", "org_id", ctx.OrgID, "error", err)
		}

		return map[string]any{
			"dashboard_id": req.DashboardID,
			"title":        title,
			"settings":     settings,
			"widgets":      widgets,
			"org_branding": orgBranding,
		}, nil
	}
}

func makeDeleteDashboardWidgetHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			WidgetID    string `json:"widget_id"`
			DashboardID string `json:"dashboard_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}
		if ctx.DashboardDocStore == nil {
			return nil, fmt.Errorf("dashboard document store not configured")
		}

		// Document widget keys are canonical lowercase UUIDs; a non-UUID can
		// never address one, so it is reported as missing rather than as a
		// document parse error.
		widgetUUID, err := uuid.Parse(req.WidgetID)
		if err != nil {
			return nil, fmt.Errorf("widget not found")
		}

		state, err := ctx.DashboardDocStore.LoadOrSeed(ctx.Context, ctx.OrgID, req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("load dashboard document: %w", err)
		}
		newState, err := dashboarddoc.DeleteWidget(state, widgetUUID.String())
		if errors.Is(err, dashboarddoc.ErrWidgetNotFound) {
			return nil, fmt.Errorf("widget not found")
		}
		if err != nil {
			return nil, fmt.Errorf("delete widget: %w", err)
		}
		if err := ctx.DashboardDocStore.Store(ctx.Context, req.DashboardID, newState); err != nil {
			return nil, fmt.Errorf("delete widget: %w", err)
		}

		_ = ctx.AuditLog("dashboard.delete_widget", "dashboard", req.DashboardID)
		return map[string]any{"status": "deleted"}, nil
	}
}

func makeUpdateDashboardHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string  `json:"dashboard_id"`
			Title       *string `json:"title"`
			GridCols    *int    `json:"grid_cols"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}
		if ctx.DashboardDocStore == nil {
			return nil, fmt.Errorf("dashboard document store not configured")
		}

		if req.Title == nil && req.GridCols == nil {
			return nil, fmt.Errorf("nothing to update")
		}

		state, err := ctx.DashboardDocStore.LoadOrSeed(ctx.Context, ctx.OrgID, req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("load dashboard document: %w", err)
		}

		// grid_cols lives in the document's settings map, and UpdateMeta
		// replaces the whole settings container: merge over the projected
		// settings first, so a grid-only update keeps every other setting
		// (variables live in their own document field and stay untouched).
		var settings map[string]any
		if req.GridCols != nil {
			proj, err := dashboarddoc.Project(state)
			if err != nil {
				return nil, fmt.Errorf("read dashboard document: %w", err)
			}
			settings = make(map[string]any, len(proj.Settings)+1)
			for k, v := range proj.Settings {
				settings[k] = v
			}
			settings["grid_cols"] = *req.GridCols
		}

		newState, err := dashboarddoc.UpdateMeta(state, req.Title, settings, nil)
		if err != nil {
			return nil, fmt.Errorf("update dashboard: %w", err)
		}
		if err := ctx.DashboardDocStore.Store(ctx.Context, req.DashboardID, newState); err != nil {
			return nil, fmt.Errorf("update dashboard: %w", err)
		}

		_ = ctx.AuditLog("dashboard.update", "dashboard", req.DashboardID)
		return map[string]any{"dashboard_id": req.DashboardID, "status": "updated"}, nil
	}
}

// makeUpdateDashboardSettingsHandler implements update_dashboard_settings:
// title/app/tabs are validated with the same validators the REST paths use,
// shallow-merged over the document's projected settings (absent keys stay),
// and written through the dashboard document in one store.
func makeUpdateDashboardSettingsHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string          `json:"dashboard_id"`
			Title       *string         `json:"title"`
			App         map[string]any  `json:"app"`
			Tabs        json.RawMessage `json:"tabs"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}
		if ctx.DashboardDocStore == nil {
			return nil, fmt.Errorf("dashboard document store not configured")
		}
		if req.Title == nil && req.App == nil && req.Tabs == nil {
			return nil, fmt.Errorf("nothing to update")
		}

		// The app block is validated as a raw map (unknown keys are stored,
		// matching the REST shallow-merge path); tabs are decoded into the
		// model so the same bounds and charset checks run.
		if req.App != nil {
			if err := validate.DashboardSettingsValue(map[string]any{"app": req.App}); err != nil {
				return nil, err
			}
			// The logo reference points at an org attachment; the existence
			// check needs the database, so it runs after the pure validators
			// (mirroring the REST update path) and before the document load,
			// so a rejected reference never seeds the document.
			if id, _ := req.App["logo_attachment_id"].(string); id != "" {
				if err := validateDashboardLogoAttachment(ctx.Context, pool, ctx.OrgID, id); err != nil {
					return nil, err
				}
			}
		}
		var tabs []models.DashboardTab
		if req.Tabs != nil {
			if err := json.Unmarshal(req.Tabs, &tabs); err != nil {
				return nil, fmt.Errorf("invalid tabs")
			}
			if tabs == nil {
				tabs = []models.DashboardTab{}
			}
			if err := validate.DashboardTabs(tabs); err != nil {
				return nil, err
			}
		}

		state, err := ctx.DashboardDocStore.LoadOrSeed(ctx.Context, ctx.OrgID, req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("load dashboard document: %w", err)
		}
		proj, err := dashboarddoc.Project(state)
		if err != nil {
			return nil, fmt.Errorf("read dashboard document: %w", err)
		}

		// UpdateMeta replaces the whole settings container: merge over the
		// projected settings first, so an app-only update keeps tabs and
		// everything else (variables live in their own document field).
		var mergedSettings map[string]any
		if req.App != nil || req.Tabs != nil {
			mergedSettings = make(map[string]any, len(proj.Settings)+2)
			for k, v := range proj.Settings {
				mergedSettings[k] = v
			}
			if req.App != nil {
				mergedSettings["app"] = req.App
			}
			if req.Tabs != nil {
				mergedSettings["tabs"] = tabs
			}
		}

		newState, err := dashboarddoc.UpdateMeta(state, req.Title, mergedSettings, nil)
		if err != nil {
			return nil, fmt.Errorf("update dashboard settings: %w", err)
		}
		if err := ctx.DashboardDocStore.Store(ctx.Context, req.DashboardID, newState); err != nil {
			return nil, fmt.Errorf("update dashboard settings: %w", err)
		}

		keys := make([]string, 0, 3)
		if req.Title != nil {
			keys = append(keys, "title")
		}
		if req.App != nil {
			keys = append(keys, "app")
		}
		if req.Tabs != nil {
			keys = append(keys, "tabs")
		}
		_ = ctx.AuditLogWithMetadata("dashboard.update", "dashboard", req.DashboardID, map[string]any{"keys": keys})
		return map[string]any{"dashboard_id": req.DashboardID, "status": "updated", "keys": keys}, nil
	}
}

// makeSetDashboardLayoutHandler implements set_dashboard_layout: tabs replace
// the App tab list (validated like the REST settings path), placements move
// widgets into tabs, and appearance patches merge into each widget's
// config.appearance. Every edit is applied to the document in memory and
// stored once, so a rejected tab or widget id leaves the document untouched.
func makeSetDashboardLayoutHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string          `json:"dashboard_id"`
			Tabs        json.RawMessage `json:"tabs"`
			Placements  []struct {
				WidgetID string `json:"widget_id"`
				Tab      string `json:"tab"`
			} `json:"placements"`
			Appearance []struct {
				WidgetID   string         `json:"widget_id"`
				Appearance map[string]any `json:"appearance"`
			} `json:"appearance"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}
		if ctx.DashboardDocStore == nil {
			return nil, fmt.Errorf("dashboard document store not configured")
		}
		if req.Tabs == nil && len(req.Placements) == 0 && len(req.Appearance) == 0 {
			return nil, fmt.Errorf("nothing to update")
		}

		// Validate and canonicalize everything that does not need the
		// document first, so a rejected request never lazily seeds it.
		var tabs []models.DashboardTab
		if req.Tabs != nil {
			if err := json.Unmarshal(req.Tabs, &tabs); err != nil {
				return nil, fmt.Errorf("invalid tabs")
			}
			if tabs == nil {
				tabs = []models.DashboardTab{}
			}
			if err := validate.DashboardTabs(tabs); err != nil {
				return nil, err
			}
		}
		for i := range req.Placements {
			if err := validate.WidgetTab(req.Placements[i].Tab); err != nil {
				return nil, err
			}
			u, err := uuid.Parse(req.Placements[i].WidgetID)
			if err != nil {
				return nil, fmt.Errorf("widget not found")
			}
			req.Placements[i].WidgetID = u.String()
		}
		for i := range req.Appearance {
			u, err := uuid.Parse(req.Appearance[i].WidgetID)
			if err != nil {
				return nil, fmt.Errorf("widget not found")
			}
			req.Appearance[i].WidgetID = u.String()
		}

		state, err := ctx.DashboardDocStore.LoadOrSeed(ctx.Context, ctx.OrgID, req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("load dashboard document: %w", err)
		}
		proj, err := dashboarddoc.Project(state)
		if err != nil {
			return nil, fmt.Errorf("read dashboard document: %w", err)
		}

		newState := state
		if req.Tabs != nil {
			mergedSettings := make(map[string]any, len(proj.Settings)+1)
			for k, v := range proj.Settings {
				mergedSettings[k] = v
			}
			mergedSettings["tabs"] = tabs
			newState, err = dashboarddoc.UpdateMeta(newState, nil, mergedSettings, nil)
			if err != nil {
				return nil, fmt.Errorf("set tabs: %w", err)
			}
		}
		for _, p := range req.Placements {
			existing, ok := proj.Widgets[p.WidgetID]
			if !ok {
				return nil, fmt.Errorf("widget not found")
			}
			layout := existing.Layout
			layout.Tab = p.Tab
			newState, err = dashboarddoc.UpdateLayout(newState, p.WidgetID, layout)
			if errors.Is(err, dashboarddoc.ErrWidgetNotFound) {
				return nil, fmt.Errorf("widget not found")
			}
			if err != nil {
				return nil, fmt.Errorf("set layout: %w", err)
			}
		}
		// Appearance entries apply sequentially over a working copy of each
		// widget's config (seeded from the projection on first touch), so two
		// entries for the same widget compose instead of the second
		// overwriting the first.
		workingConfigs := make(map[string]map[string]any, len(req.Appearance))
		for _, a := range req.Appearance {
			existing, ok := proj.Widgets[a.WidgetID]
			if !ok {
				return nil, fmt.Errorf("widget not found")
			}
			// Config is a whole-value document field, so the appearance patch
			// is merged over the widget's working config and the whole map is
			// written back.
			config, ok := workingConfigs[a.WidgetID]
			if !ok {
				config = make(map[string]any, len(existing.Config)+1)
				for k, v := range existing.Config {
					config[k] = v
				}
				workingConfigs[a.WidgetID] = config
			}
			appearance := map[string]any{}
			if current, ok := config["appearance"].(map[string]any); ok {
				for k, v := range current {
					appearance[k] = v
				}
			}
			for k, v := range a.Appearance {
				appearance[k] = v
			}
			config["appearance"] = appearance
			newState, err = dashboarddoc.SetWidgetConfig(newState, a.WidgetID, config)
			if errors.Is(err, dashboarddoc.ErrWidgetNotFound) {
				return nil, fmt.Errorf("widget not found")
			}
			if err != nil {
				return nil, fmt.Errorf("set appearance: %w", err)
			}
		}

		if err := ctx.DashboardDocStore.Store(ctx.Context, req.DashboardID, newState); err != nil {
			return nil, fmt.Errorf("set dashboard layout: %w", err)
		}

		keys := make([]string, 0, 3)
		if req.Tabs != nil {
			keys = append(keys, "tabs")
		}
		if len(req.Placements) > 0 {
			keys = append(keys, "placements")
		}
		if len(req.Appearance) > 0 {
			keys = append(keys, "appearance")
		}
		_ = ctx.AuditLogWithMetadata("dashboard.update", "dashboard", req.DashboardID, map[string]any{"keys": keys})
		return map[string]any{"dashboard_id": req.DashboardID, "status": "updated", "keys": keys}, nil
	}
}

// makePublishDashboardHandler implements publish_dashboard. The snapshot,
// content hash, and version numbering are shared with the REST publish path
// through dashboarddoc.PublishVersion, so the two can never diverge.
func makePublishDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string `json:"dashboard_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}
		if ctx.DashboardDocStore == nil {
			return nil, fmt.Errorf("dashboard document store not configured")
		}

		state, err := ctx.DashboardDocStore.LoadOrSeed(ctx.Context, ctx.OrgID, req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("load dashboard document: %w", err)
		}
		version, contentHash, createdAt, err := dashboarddoc.PublishVersion(ctx.Context, pool, ctx.OrgID, req.DashboardID, ctx.UserID, state)
		if err != nil {
			var warnings *dashboarddoc.SnapshotWarningsError
			if errors.As(err, &warnings) {
				return nil, fmt.Errorf("dashboard cannot be published: %s", strings.Join(warnings.Warnings, "; "))
			}
			return nil, fmt.Errorf("publish dashboard: %w", err)
		}

		_ = ctx.AuditLogWithMetadata("dashboard.publish", "dashboard", req.DashboardID, map[string]any{
			"version":      version,
			"content_hash": contentHash,
		})
		return map[string]any{
			"dashboard_id": req.DashboardID,
			"version":      version,
			"content_hash": contentHash,
			"created_at":   createdAt,
		}, nil
	}
}

func makeShareDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string `json:"dashboard_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "view"); err != nil {
			return nil, err
		}

		var token string
		err := pool.QueryRow(ctx.Context,
			`SELECT token FROM public_tokens WHERE resource_type='dashboard' AND resource_id=$1`,
			req.DashboardID,
		).Scan(&token)
		if err != nil {
			tokenBytes := make([]byte, 16)
			cryptorand.Read(tokenBytes)
			token = hex.EncodeToString(tokenBytes)

			_, err = pool.Exec(ctx.Context,
				`INSERT INTO public_tokens (org_id, resource_type, resource_id, token, created_by)
				 VALUES ($1, 'dashboard', $2, $3, $4)`,
				ctx.OrgID, req.DashboardID, token, ctx.UserID)
			if err != nil {
				return nil, fmt.Errorf("failed to create share link")
			}
		}

		_ = ctx.AuditLog("dashboard.share", "dashboard", req.DashboardID)
		return map[string]any{"token": token, "url": "/public/" + token}, nil
	}
}

func makeListDashboardsHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		rows, err := pool.Query(ctx.Context,
			`SELECT id, title, created_at FROM dashboards WHERE org_id=$1 ORDER BY created_at DESC LIMIT 50`,
			ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("list dashboards: %w", err)
		}
		defer rows.Close()

		var dashboards []map[string]any
		for rows.Next() {
			var id, title string
			var createdAt time.Time
			if err := rows.Scan(&id, &title, &createdAt); err != nil {
				return nil, fmt.Errorf("scan dashboard: %w", err)
			}
			dashboards = append(dashboards, map[string]any{
				"id": id, "title": title, "created_at": createdAt.Format(time.RFC3339),
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("list dashboards iter: %w", err)
		}
		if dashboards == nil {
			dashboards = []map[string]any{}
		}
		return map[string]any{"dashboards": dashboards, "count": len(dashboards)}, nil
	}
}

func makeDeleteDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string `json:"dashboard_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "delete"); err != nil {
			return nil, err
		}
		result, err := pool.Exec(ctx.Context,
			`DELETE FROM dashboards WHERE id=$1 AND org_id=$2`,
			req.DashboardID, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("delete dashboard: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, fmt.Errorf("dashboard not found")
		}
		_ = ctx.AuditLog("dashboard.delete", "dashboard", req.DashboardID)

		// The row is gone for good: tell relay replicas to disconnect viewers
		// and unload any in-memory document so a stale live copy cannot outlive
		// it (the REST delete publishes "trashed" for the same reason).
		// Best-effort and optional: a bare context has no store, and a failed
		// broadcast never fails the delete.
		if ctx.DashboardDocStore != nil {
			ctx.DashboardDocStore.Invalidate(ctx.Context, req.DashboardID)
		}
		return map[string]any{"dashboard_id": req.DashboardID, "status": "deleted"}, nil
	}
}

func makeCreateScheduleHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID     string `json:"notebook_id"`
			CronExpression string `json:"cron_expression"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.CronExpression == "" {
			return nil, fmt.Errorf("cron_expression is required")
		}
		if err := ctx.CheckPermission("notebook", req.NotebookID, "edit"); err != nil {
			return nil, err
		}

		id := uuid.New().String()
		now := time.Now()
		_, err := pool.Exec(ctx.Context, `
			INSERT INTO schedules (id, notebook_id, cron_expression, enabled, created_at, updated_at)
			VALUES ($1, $2, $3, true, $4, $4)
		`, id, req.NotebookID, req.CronExpression, now)
		if err != nil {
			return nil, fmt.Errorf("create schedule: %w", err)
		}

		_ = ctx.AuditLog("schedule.create", "schedule", id)
		return map[string]any{"schedule_id": id, "cron_expression": req.CronExpression}, nil
	}
}

func makeDeleteScheduleHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ScheduleID string `json:"schedule_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		var nbID string
		if err := pool.QueryRow(ctx.Context,
			`SELECT notebook_id FROM schedules WHERE id=$1`, req.ScheduleID).Scan(&nbID); err != nil {
			return nil, fmt.Errorf("schedule not found")
		}
		if err := ctx.CheckPermission("notebook", nbID, "edit"); err != nil {
			return nil, err
		}

		result, err := pool.Exec(ctx.Context,
			`DELETE FROM schedules WHERE id=$1 AND notebook_id IN (SELECT id FROM notebooks WHERE org_id=$2)`,
			req.ScheduleID, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("delete schedule: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, fmt.Errorf("schedule not found")
		}
		_ = ctx.AuditLog("schedule.delete", "schedule", req.ScheduleID)
		return map[string]any{"schedule_id": req.ScheduleID, "status": "deleted"}, nil
	}
}

func makeReadPermissionsHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ResourceType string `json:"resource_type"`
			ResourceID   string `json:"resource_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission(req.ResourceType, req.ResourceID, "manage"); err != nil {
			return nil, err
		}

		rows, err := pool.Query(ctx.Context, `
			SELECT id, subject_type, subject_id, actions FROM acl_entries
			WHERE resource_type=$1 AND resource_id=$2 AND org_id=$3
			ORDER BY subject_type, subject_id
		`, req.ResourceType, req.ResourceID, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("query permissions: %w", err)
		}
		defer rows.Close()

		type entry struct {
			ID          string   `json:"id"`
			SubjectType string   `json:"subject_type"`
			SubjectID   *string  `json:"subject_id"`
			Actions     []string `json:"actions"`
		}
		var entries []entry
		for rows.Next() {
			var e entry
			var actions []byte
			if err := rows.Scan(&e.ID, &e.SubjectType, &e.SubjectID, &actions); err != nil {
				return nil, fmt.Errorf("scan entry: %w", err)
			}
			json.Unmarshal(actions, &e.Actions)
			entries = append(entries, e)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("read permissions iter: %w", err)
		}
		if entries == nil {
			entries = []entry{}
		}
		return map[string]any{"entries": entries}, nil
	}
}

func makeUpdatePermissionsHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ResourceType string `json:"resource_type"`
			ResourceID   string `json:"resource_id"`
			Entries      []struct {
				SubjectType string   `json:"subject_type"`
				SubjectID   *string  `json:"subject_id"`
				Actions     []string `json:"actions"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		// Session ACLs enforce owner preservation and view-only shares in the
		// ACL API; this raw replace path would bypass both, so agent_session is
		// rejected outright rather than routed around those rules.
		if req.ResourceType == "agent_session" {
			return nil, fmt.Errorf("agent session sharing must use the ACL API: sessions are read-only when shared")
		}
		if err := ctx.CheckPermission(req.ResourceType, req.ResourceID, "manage"); err != nil {
			return nil, err
		}

		tx, err := pool.Begin(ctx.Context)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(context.Background())

		tx.Exec(ctx.Context, `DELETE FROM acl_entries WHERE resource_type=$1 AND resource_id=$2 AND org_id=$3`,
			req.ResourceType, req.ResourceID, ctx.OrgID)

		for _, e := range req.Entries {
			actionsJSON, _ := json.Marshal(e.Actions)
			subjType := e.SubjectType
			var subjID *string
			if e.SubjectType == "everyone" {
				subjType = "org_role"
				s := "everyone"
				subjID = &s
			} else {
				subjID = e.SubjectID
			}
			_, err := tx.Exec(ctx.Context, `
				INSERT INTO acl_entries (resource_type, resource_id, org_id, subject_type, subject_id, actions)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, req.ResourceType, req.ResourceID, ctx.OrgID, subjType, subjID, actionsJSON)
			if err != nil {
				return nil, fmt.Errorf("insert entry: %w", err)
			}
		}

		if err := tx.Commit(ctx.Context); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}

		_ = ctx.AuditLog("acl.update", req.ResourceType, req.ResourceID)
		return map[string]any{"status": "updated"}, nil
	}
}

func makeExportNotebookHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID string `json:"notebook_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("notebook", req.NotebookID, "view"); err != nil {
			return nil, err
		}

		var title string
		if err := pool.QueryRow(ctx.Context, `SELECT title FROM notebooks WHERE id=$1`, req.NotebookID).Scan(&title); err != nil {
			return nil, fmt.Errorf("notebook not found: %w", err)
		}

		rows, err := pool.Query(ctx.Context,
			`SELECT type, language, source, position FROM cells WHERE notebook_id=$1 ORDER BY position`,
			req.NotebookID)
		if err != nil {
			return nil, fmt.Errorf("query cells: %w", err)
		}
		defer rows.Close()

		type ipynbCell struct {
			CellType string `json:"cell_type"`
			Source   string `json:"source"`
			Metadata struct {
				Language string `json:"language,omitempty"`
			} `json:"metadata"`
		}
		var cells []ipynbCell
		for rows.Next() {
			var c ipynbCell
			var cellType, lang, src string
			var pos int
			if err := rows.Scan(&cellType, &lang, &src, &pos); err != nil {
				return nil, fmt.Errorf("scan cell: %w", err)
			}
			c.CellType = cellType
			c.Source = src
			c.Metadata.Language = lang
			cells = append(cells, c)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("export cells iter: %w", err)
		}

		ipynb := map[string]any{
			"nbformat":       4,
			"nbformat_minor": 5,
			"metadata": map[string]any{
				"title": title,
			},
			"cells": cells,
		}

		return ipynb, nil
	}
}

func makeImportNotebookHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Title     string  `json:"title"`
			IPynbJSON string  `json:"ipynb_json"`
			FolderID  *string `json:"folder_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		var ipynb struct {
			Cells []struct {
				CellType string `json:"cell_type"`
				Source   string `json:"source"`
				Metadata struct {
					Language string `json:"language"`
				} `json:"metadata"`
			} `json:"cells"`
		}
		if err := json.Unmarshal([]byte(req.IPynbJSON), &ipynb); err != nil {
			return nil, fmt.Errorf("invalid ipynb JSON: %w", err)
		}

		if req.FolderID != nil && *req.FolderID != "" {
			if err := ctx.CheckPermission("folder", *req.FolderID, "edit"); err != nil {
				return nil, err
			}
		}

		now := time.Now()
		nbID := uuid.New().String()

		tx, err := pool.Begin(ctx.Context)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx.Context)

		_, err = tx.Exec(ctx.Context, `
			INSERT INTO notebooks (id, org_id, title, description, created_by, folder_id, created_at, updated_at)
			VALUES ($1, $2, $3, '', $4, $5, $6, $6)
		`, nbID, ctx.OrgID, req.Title, ctx.UserID, req.FolderID, now)
		if err != nil {
			return nil, fmt.Errorf("create notebook: %w", err)
		}

		for i, c := range ipynb.Cells {
			cellType := c.CellType
			lang := c.Metadata.Language
			if cellType == "code" && lang == "" {
				lang = "sql"
			}
			if cellType == "markdown" && lang == "" {
				lang = "markdown"
			}
			cellID := uuid.New().String()
			if _, err := tx.Exec(ctx.Context, `
				INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
			`, cellID, nbID, cellType, lang, c.Source, i, now); err != nil {
				return nil, fmt.Errorf("create cell %d: %w", i, err)
			}
		}

		if _, err := tx.Exec(ctx.Context, `
			INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['view','run','edit','share','delete','create'])
			ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
			ctx.OrgID, nbID, ctx.UserID,
		); err != nil {
			return nil, fmt.Errorf("seed ACL entries for imported notebook: %w", err)
		}

		if err := tx.Commit(ctx.Context); err != nil {
			return nil, fmt.Errorf("commit import: %w", err)
		}

		_ = ctx.AuditLog("notebook.import", "notebook", nbID)
		return map[string]any{"notebook_id": nbID, "title": req.Title, "cells": len(ipynb.Cells)}, nil
	}
}
