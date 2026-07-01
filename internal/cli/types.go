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
		ID        string `json:"id"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		IsDefault bool   `json:"is_default"`
		CreatedAt string `json:"created_at"`
	}

	Group struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
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
		ID        string    `json:"id"`
		Action    string    `json:"action"`
		UserEmail string    `json:"user_email"`
		Resource  string    `json:"resource"`
		CreatedAt time.Time `json:"created_at"`
	}

	MOTD struct {
		ID        string    `json:"id"`
		Message   string    `json:"message"`
		Active    bool      `json:"active"`
		CreatedAt time.Time `json:"created_at"`
	}

	Agent struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Description   string `json:"description"`
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
		ID   string `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}

	SSOProvider struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		IssuerURL     string `json:"issuer_url"`
		ClientID      string `json:"client_id"`
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
