package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/agent"
	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/models"
)

type modelConfigHandlers struct {
	server *Server
}

// @Summary List model configs
// @Description List all model configurations for the organization
// @Tags model-configs
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /model-configs [get]
func (h *modelConfigHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT id, org_id, name, provider, base_url, model, api_key_encrypted,
			   default_params, context_window, folder_id, created_by, created_at, updated_at
		FROM model_configs WHERE org_id = $1 ORDER BY name
	`, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	configs := []models.ModelConfig{}
	for rows.Next() {
		var c models.ModelConfig
		var defaultParams []byte
		rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Provider, &c.BaseURL, &c.Model, &c.APIKeyEncrypted,
			&defaultParams, &c.ContextWindow, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
		json.Unmarshal(defaultParams, &c.DefaultParams)
		allowed, _ := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "model_config", c.ID, "view")
		if !allowed {
			continue
		}
		configs = append(configs, c)
	}

	for i := range configs {
		configs[i].APIKeyEncrypted = nil
	}

	writeJSON(w, http.StatusOK, configs)
}

// @Summary Create a model config
// @Description Create a new model configuration
// @Tags model-configs
// @Accept json
// @Produce json
// @Param request body object true "Model config details"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /model-configs [post]
func (h *modelConfigHandlers) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "model_config", id, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var c models.ModelConfig
	var apiKeyEncrypted []byte
	var defaultParams []byte
	err = h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, org_id, name, provider, base_url, model, api_key_encrypted,
			   default_params, context_window, folder_id, created_by, created_at, updated_at
		FROM model_configs WHERE id = $1 AND org_id = $2
	`, id, claims.OrgID).Scan(&c.ID, &c.OrgID, &c.Name, &c.Provider, &c.BaseURL, &c.Model, &apiKeyEncrypted,
		&defaultParams, &c.ContextWindow, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "model config not found")
		return
	}
	json.Unmarshal(defaultParams, &c.DefaultParams)
	c.APIKeyEncrypted = nil

	writeJSON(w, http.StatusOK, c)
}

func (h *modelConfigHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Name          string         `json:"name"`
		Provider      string         `json:"provider"`
		BaseURL       string         `json:"base_url"`
		Model         string         `json:"model"`
		APIKey        string         `json:"api_key"`
		DefaultParams models.JSONMap `json:"default_params"`
		ContextWindow int            `json:"context_window"`
		FolderID      *string        `json:"folder_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	encryptedKey, err := crypto.Encrypt([]byte(req.APIKey), h.server.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt API key")
		return
	}

	if req.ContextWindow == 0 {
		req.ContextWindow = 128000
	}

	cfgID := uuid.New().String()
	defaultParamsJSON, _ := json.Marshal(req.DefaultParams)

	_, err = h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted,
			default_params, context_window, folder_id, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
	`, cfgID, claims.OrgID, req.Name, req.Provider, req.BaseURL, req.Model, encryptedKey,
		defaultParamsJSON, req.ContextWindow, req.FolderID, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Grant creator full access
	h.server.db.Pool.Exec(r.Context(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'model_config', $2, 'user', $3, ARRAY['view','edit','delete'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, cfgID, claims.UserID)

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "model_config.create", ResourceType: "model_config", ResourceID: cfgID,
	})

	writeJSON(w, http.StatusCreated, map[string]string{"id": cfgID})
}

// @Summary Update a model config
// @Description Update a model configuration
// @Tags model-configs
// @Accept json
// @Produce json
// @Param id path string true "Model Config ID"
// @Param request body object true "Model config updates"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /model-configs/{id} [put]
func (h *modelConfigHandlers) handleUpdate(w http.ResponseWriter, r *http.Request) {
	cfgID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var req struct {
		Name          *string         `json:"name"`
		Provider      *string         `json:"provider"`
		BaseURL       *string         `json:"base_url"`
		Model         *string         `json:"model"`
		APIKey        *string         `json:"api_key"`
		DefaultParams *models.JSONMap `json:"default_params"`
		ContextWindow *int            `json:"context_window"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.APIKey != nil {
		encrypted, err := crypto.Encrypt([]byte(*req.APIKey), h.server.masterKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt API key")
			return
		}
		result, err := h.server.db.Pool.Exec(r.Context(), `
			UPDATE model_configs SET api_key_encrypted = $2, updated_at = NOW() WHERE id = $1 AND org_id = $3
		`, cfgID, encrypted, claims.OrgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "model config not found")
			return
		}
	}

	defaultParamsJSON, _ := json.Marshal(req.DefaultParams)

	result, err := h.server.db.Pool.Exec(r.Context(), `
		UPDATE model_configs SET
			name = COALESCE($2, name),
			provider = COALESCE($3, provider),
			base_url = COALESCE($4, base_url),
			model = COALESCE($5, model),
			default_params = COALESCE($6, default_params),
			context_window = COALESCE($7, context_window),
			updated_at = NOW()
		WHERE id = $1 AND org_id = $8
	`, cfgID, req.Name, req.Provider, req.BaseURL, req.Model, defaultParamsJSON, req.ContextWindow, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "model config not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"id": cfgID})
}

func (h *modelConfigHandlers) handleDelete(w http.ResponseWriter, r *http.Request) {
	cfgID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	result, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM model_configs WHERE id = $1 AND org_id = $2`, cfgID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "model config not found")
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}

func (h *modelConfigHandlers) handleTest(w http.ResponseWriter, r *http.Request) {
	cfgID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	if allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "model_config", cfgID, "view"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var mc models.ModelConfig
	var apiKeyEncrypted []byte
	var defaultParams []byte
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, folder_id, created_by, created_at, updated_at
		FROM model_configs WHERE id = $1 AND org_id = $2
	`, cfgID, claims.OrgID).Scan(&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &apiKeyEncrypted, &defaultParams, &mc.ContextWindow, &mc.FolderID, &mc.CreatedBy, &mc.CreatedAt, &mc.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "model config not found")
		return
	}

	llmClient := agent.NewLLMClient(mc.BaseURL, mc.Model, apiKeyEncrypted)
	resp, err := llmClient.Chat(r.Context(), []agent.ChatMessage{{Role: "user", Content: "Say 'OK' if you can hear me."}}, nil, h.server.masterKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "connection failed: "+err.Error())
		return
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		writeError(w, http.StatusBadGateway, "unexpected response: no content")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"response": resp.Choices[0].Message.Content,
		"model":    resp.Model,
		"usage":    resp.Usage,
	})
}
