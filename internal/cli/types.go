package cli

import (
	"encoding/json"
	"time"
)

type (
	Notebook struct {
		ID          string      `json:"id"`
		OrgID       string      `json:"org_id"`
		Title       string      `json:"title"`
		Description string      `json:"description"`
		FolderID    *string     `json:"folder_id,omitempty"`
		Parameters  []Parameter `json:"parameters"`
		CreatedBy   string      `json:"created_by"`
		CreatedAt   time.Time   `json:"created_at"`
		UpdatedAt   time.Time   `json:"updated_at"`
	}

	Parameter struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Default string `json:"default"`
	}

	Cell struct {
		ID             string          `json:"id"`
		NotebookID     string          `json:"notebook_id"`
		Position       int             `json:"position"`
		Type           string          `json:"type"`
		Language       string          `json:"language,omitempty"`
		ConnectorID    string          `json:"connector_id,omitempty"`
		Source         string          `json:"source"`
		Outputs        json.RawMessage `json:"outputs"`
		SourceVisible  bool            `json:"source_visible"`
		OutputsHidden  bool            `json:"outputs_hidden"`
		CellCollapsed  bool            `json:"cell_collapsed"`
		SlideBreak     bool            `json:"slide_break"`
		Title          string          `json:"title,omitempty"`
		Description    string          `json:"description,omitempty"`
		Slug           string          `json:"slug,omitempty"`
		Limit          *int            `json:"limit,omitempty"`
		CreatedAt      time.Time       `json:"created_at"`
		UpdatedAt      time.Time       `json:"updated_at"`
		AgentUpdatedAt *time.Time      `json:"agent_updated_at,omitempty"`
	}

	Output struct {
		Type   string      `json:"type"`
		Data   interface{} `json:"data,omitempty"`
		Config interface{} `json:"config,omitempty"`
	}

	CellVersion struct {
		ID        string    `json:"id"`
		CellID    string    `json:"cell_id"`
		Source    string    `json:"source"`
		CreatedAt time.Time `json:"created_at"`
		CreatedBy string    `json:"created_by"`
	}

	Folder struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		ParentID  *string   `json:"parent_id,omitempty"`
		OwnerID   *string   `json:"owner_id,omitempty"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	Dashboard struct {
		ID        string            `json:"id"`
		OrgID     string            `json:"org_id"`
		Title     string            `json:"title"`
		Settings  DashboardSettings `json:"settings"`
		FolderID  *string           `json:"folder_id,omitempty"`
		CreatedBy string            `json:"created_by"`
		CreatedAt time.Time         `json:"created_at"`
		UpdatedAt time.Time         `json:"updated_at"`
	}

	DashboardSettings struct {
		AutoRefreshSeconds int               `json:"auto_refresh_seconds,omitempty"`
		ParameterOverrides map[string]string `json:"parameter_overrides,omitempty"`
		GridCols           int               `json:"grid_cols,omitempty"`
	}

	Widget struct {
		ID          string                 `json:"id"`
		DashboardID string                 `json:"dashboard_id"`
		NotebookID  *string                `json:"notebook_id,omitempty"`
		CellID      *string                `json:"cell_id,omitempty"`
		Type        string                 `json:"type"`
		Layout      WidgetLayout           `json:"layout"`
		Config      map[string]interface{} `json:"config"`
		CreatedAt   time.Time              `json:"created_at"`
		UpdatedAt   time.Time              `json:"updated_at"`
	}

	WidgetLayout struct {
		Row    int `json:"row"`
		Col    int `json:"col"`
		Width  int `json:"width"`
		Height int `json:"height"`
	}

	Connector struct {
		ID             string    `json:"id"`
		OrgID          string    `json:"org_id"`
		Name           string    `json:"name"`
		Type           string    `json:"type"`
		MaxRows        int       `json:"max_rows"`
		TimeoutSeconds int       `json:"timeout_seconds"`
		IsDefault      bool      `json:"is_default"`
		FolderID       *string   `json:"folder_id,omitempty"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
	}

	Group struct {
		ID          string    `json:"id"`
		OrgID       string    `json:"org_id"`
		Name        string    `json:"name"`
		MemberCount int       `json:"member_count"`
		CreatedAt   time.Time `json:"created_at"`
	}

	GroupMember struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
		Name   string `json:"name"`
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
		ID          string            `json:"id"`
		NotebookID  string            `json:"notebook_id"`
		Name        string            `json:"name"`
		Title       string            `json:"title"`
		CellSources map[string]string `json:"cell_sources"`
		Cells       []SnapshotCell    `json:"cells,omitempty"`
		CreatedBy   string            `json:"created_by"`
		CreatedAt   time.Time         `json:"created_at"`
		Auto        bool              `json:"auto"`
		Changes     *SnapshotChanges  `json:"changes,omitempty"`
	}

	SnapshotCell struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Language    string `json:"language,omitempty"`
		Source      string `json:"source"`
		Position    int    `json:"position"`
		ConnectorID string `json:"connector_id,omitempty"`
	}

	SnapshotChanges struct {
		CellsAdded    []CellChange `json:"cells_added"`
		CellsDeleted  []CellChange `json:"cells_deleted"`
		CellsModified []CellChange `json:"cells_modified"`
	}

	CellChange struct {
		ID       string `json:"id"`
		Position int    `json:"position"`
	}

	Schedule struct {
		ID                 string            `json:"id"`
		NotebookID         string            `json:"notebook_id"`
		CronExpression     string            `json:"cron_expression"`
		ParameterOverrides map[string]string `json:"parameter_overrides"`
		Enabled            bool              `json:"enabled"`
		LastRunAt          *time.Time        `json:"last_run_at,omitempty"`
		NextRunAt          *time.Time        `json:"next_run_at,omitempty"`
		CreatedAt          time.Time         `json:"created_at"`
		UpdatedAt          time.Time         `json:"updated_at"`
	}

	PAT struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Token     string    `json:"token,omitempty"`
		CreatedAt time.Time `json:"created_at"`
	}

	AuditLog struct {
		ID                 int64          `json:"id"`
		OrgID              string         `json:"org_id"`
		UserID             string         `json:"user_id,omitempty"`
		UserEmail          string         `json:"user_email,omitempty"`
		Action             string         `json:"action"`
		ResourceType       string         `json:"resource_type"`
		ResourceID         string         `json:"resource_id,omitempty"`
		ResourceName       string         `json:"resource_name,omitempty"`
		ResourceParentName string         `json:"resource_parent_name,omitempty"`
		Metadata           map[string]any `json:"metadata,omitempty"`
		CreatedAt          time.Time      `json:"created_at"`
	}

	MOTD struct {
		ID          string     `json:"id"`
		Title       string     `json:"title"`
		Content     string     `json:"content"`
		Priority    int        `json:"priority"`
		Visibility  string     `json:"visibility"`
		Pages       []string   `json:"pages"`
		ShowOnLogin bool       `json:"show_on_login"`
		CreatedAt   time.Time  `json:"created_at"`
		ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	}

	Agent struct {
		ID                    string    `json:"id"`
		OrgID                 string    `json:"org_id"`
		Name                  string    `json:"name"`
		Description           string    `json:"description,omitempty"`
		ModelConfigID         *string   `json:"model_config_id,omitempty"`
		SubagentModelConfigID *string   `json:"subagent_model_config_id,omitempty"`
		SystemPrompt          string    `json:"system_prompt,omitempty"`
		SkillIDs              []string  `json:"skill_ids"`
		MCPServerIDs          []string  `json:"mcp_server_ids"`
		ToolIDs               []string  `json:"tool_ids,omitempty"`
		FolderID              *string   `json:"folder_id,omitempty"`
		MaxTurns              *int      `json:"max_turns,omitempty"`
		CreatedBy             string    `json:"created_by"`
		CreatedAt             time.Time `json:"created_at"`
		UpdatedAt             time.Time `json:"updated_at"`
	}

	AgentSession struct {
		ID         string     `json:"id"`
		AgentID    string     `json:"agent_id"`
		NotebookID string     `json:"notebook_id"`
		UserID     string     `json:"user_id"`
		MaxTurns   int        `json:"max_turns"`
		Title      *string    `json:"title,omitempty"`
		EndedAt    *time.Time `json:"ended_at,omitempty"`
		CreatedAt  time.Time  `json:"created_at"`
	}

	AgentMessage struct {
		ID               string    `json:"id"`
		SessionID        string    `json:"session_id"`
		Role             string    `json:"role"`
		Content          string    `json:"content,omitempty"`
		ToolCallID       *string   `json:"tool_call_id,omitempty"`
		ReasoningContent string    `json:"reasoning_content,omitempty"`
		TokensInput      int       `json:"tokens_input,omitempty"`
		TokensOutput     int       `json:"tokens_output,omitempty"`
		TokensReasoning  int       `json:"tokens_reasoning,omitempty"`
		DurationMs       int       `json:"duration_ms,omitempty"`
		CreatedAt        time.Time `json:"created_at"`
	}

	ModelConfig struct {
		ID            string                 `json:"id"`
		OrgID         string                 `json:"org_id"`
		Name          string                 `json:"name"`
		Provider      string                 `json:"provider"`
		BaseURL       string                 `json:"base_url"`
		Model         string                 `json:"model"`
		DefaultParams map[string]interface{} `json:"default_params"`
		ContextWindow int                    `json:"context_window"`
		FolderID      *string                `json:"folder_id,omitempty"`
		CreatedBy     string                 `json:"created_by"`
		CreatedAt     time.Time              `json:"created_at"`
		UpdatedAt     time.Time              `json:"updated_at"`
		APIKeyEnvVar  *string                `json:"-"`
	}

	Skill struct {
		ID           string    `json:"id"`
		OrgID        string    `json:"org_id"`
		Name         string    `json:"name"`
		Description  string    `json:"description,omitempty"`
		SystemPrompt string    `json:"system_prompt,omitempty"`
		FolderID     *string   `json:"folder_id,omitempty"`
		CreatedBy    string    `json:"created_by"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
	}

	Tool struct {
		ID                  string                 `json:"id"`
		OrgID               string                 `json:"org_id"`
		Name                string                 `json:"name"`
		Description         string                 `json:"description"`
		Type                string                 `json:"type"`
		Schema              map[string]interface{} `json:"schema"`
		Config              map[string]interface{} `json:"config"`
		RequireConfirmation bool                   `json:"require_confirmation"`
		FolderID            *string                `json:"folder_id,omitempty"`
		CreatedBy           string                 `json:"created_by"`
		CreatedAt           time.Time              `json:"created_at"`
		UpdatedAt           time.Time              `json:"updated_at"`
	}

	MCPServer struct {
		ID        string    `json:"id"`
		OrgID     string    `json:"org_id"`
		Name      string    `json:"name"`
		Type      string    `json:"type"`
		Command   string    `json:"command"`
		Args      []string  `json:"args,omitempty"`
		CreatedBy string    `json:"created_by"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	SSOProvider struct {
		ID             string    `json:"id"`
		Scope          string    `json:"scope"`
		Name           string    `json:"name"`
		ProviderType   string    `json:"provider_type"`
		ClientID       string    `json:"client_id"`
		DiscoveryURL   string    `json:"discovery_url"`
		AllowedDomains []string  `json:"allowed_domains"`
		Enabled        bool      `json:"enabled"`
		Scopes         []string  `json:"scopes"`
		GroupsClaim    string    `json:"groups_claim"`
		GroupPrefix    string    `json:"group_prefix"`
		AutoSyncGroups bool      `json:"auto_sync_groups"`
		GetUserInfo    bool      `json:"get_user_info"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
	}

	Org struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Slug        string    `json:"slug"`
		MemberCount int       `json:"member_count"`
		CreatedAt   time.Time `json:"created_at"`
	}

	User struct {
		ID              string    `json:"id"`
		Email           string    `json:"email"`
		Name            string    `json:"name"`
		IsPlatformAdmin bool      `json:"is_platform_admin"`
		CreatedAt       time.Time `json:"created_at"`
		Orgs            []string  `json:"orgs"`
	}

	Attachment struct {
		ID          string    `json:"id"`
		FileName    string    `json:"file_name"`
		FileSize    int64     `json:"file_size"`
		ContentType string    `json:"content_type"`
		CreatedAt   time.Time `json:"created_at"`
	}

	Template struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	}
)
