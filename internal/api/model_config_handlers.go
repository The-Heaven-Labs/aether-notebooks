package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/models"
)

// parseEnvVarRef checks if s matches ${VAR_NAME} and returns the var name,
// or empty string if it's not an env var reference.
func parseEnvVarRef(s string) string {
	if len(s) > 2 && s[0] == '$' && s[1] == '{' && s[len(s)-1] == '}' {
		name := s[2 : len(s)-1]
		if name != "" && !strings.ContainsAny(name, " \t\n{}") {
			return name
		}
	}
	return ""
}

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
			   default_params, context_window, price_per_input_token, price_per_output_token,
			   price_per_cache_read_token, folder_id, created_by, created_at, updated_at,
			   api_key_env_var
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
			&defaultParams, &c.ContextWindow, &c.PricePerInputToken, &c.PricePerOutputToken,
			&c.PricePerCacheReadToken, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
			&c.APIKeyEnvVar)
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

// @Summary Get a model config
// @Description Get a model configuration by ID
// @Tags model-configs
// @Produce json
// @Param id path string true "Model Config ID"
// @Success 200 {object} models.ModelConfig
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /model-configs/{id} [get]
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
			   default_params, context_window, price_per_input_token, price_per_output_token,
			   price_per_cache_read_token, folder_id, created_by, created_at, updated_at,
			   api_key_env_var
		FROM model_configs WHERE id = $1 AND org_id = $2
	`, id, claims.OrgID).Scan(&c.ID, &c.OrgID, &c.Name, &c.Provider, &c.BaseURL, &c.Model, &apiKeyEncrypted,
		&defaultParams, &c.ContextWindow, &c.PricePerInputToken, &c.PricePerOutputToken,
		&c.PricePerCacheReadToken, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		&c.APIKeyEnvVar)
	if err != nil {
		writeError(w, http.StatusNotFound, "model config not found")
		return
	}
	json.Unmarshal(defaultParams, &c.DefaultParams)
	c.APIKeyEncrypted = nil

	writeJSON(w, http.StatusOK, c)
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
func (h *modelConfigHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Name                   string         `json:"name"`
		Provider               string         `json:"provider"`
		BaseURL                string         `json:"base_url"`
		Model                  string         `json:"model"`
		APIKey                 string         `json:"api_key"`
		DefaultParams          models.JSONMap `json:"default_params"`
		ContextWindow          int            `json:"context_window"`
		PricePerInputToken     float64        `json:"price_per_input_token"`
		PricePerOutputToken    float64        `json:"price_per_output_token"`
		PricePerCacheReadToken float64        `json:"price_per_cache_read_token"`
		FolderID               *string        `json:"folder_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.ContextWindow == 0 {
		req.ContextWindow = 128000
	}

	if req.FolderID != nil && *req.FolderID == "" {
		req.FolderID = nil
	}

	cfgID := uuid.New().String()
	defaultParamsJSON, _ := json.Marshal(req.DefaultParams)

	var encryptedKey []byte
	var apiKeyEnvVar *string
	var err error
	if envVar := parseEnvVarRef(req.APIKey); envVar != "" {
		apiKeyEnvVar = &envVar
		val, ok := os.LookupEnv(envVar)
		if !ok || val == "" {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("environment variable %q is not set or empty", envVar))
			return
		}
		encryptedKey, err = crypto.Encrypt([]byte(val), h.server.masterKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt API key")
			return
		}
	} else {
		encryptedKey, err = crypto.Encrypt([]byte(req.APIKey), h.server.masterKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt API key")
			return
		}
	}

	_, err = h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted,
			default_params, context_window, price_per_input_token, price_per_output_token,
			price_per_cache_read_token, folder_id, created_by, created_at, updated_at,
			api_key_env_var)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW(), NOW(), $15)
	`, cfgID, claims.OrgID, req.Name, req.Provider, req.BaseURL, req.Model, encryptedKey,
		defaultParamsJSON, req.ContextWindow, req.PricePerInputToken, req.PricePerOutputToken,
		req.PricePerCacheReadToken, req.FolderID, claims.UserID, apiKeyEnvVar)
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
		Name                   *string         `json:"name"`
		Provider               *string         `json:"provider"`
		BaseURL                *string         `json:"base_url"`
		Model                  *string         `json:"model"`
		APIKey                 *string         `json:"api_key"`
		DefaultParams          *models.JSONMap `json:"default_params"`
		ContextWindow          *int            `json:"context_window"`
		PricePerInputToken     *float64        `json:"price_per_input_token"`
		PricePerOutputToken    *float64        `json:"price_per_output_token"`
		PricePerCacheReadToken *float64        `json:"price_per_cache_read_token"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.APIKey != nil {
		if envVar := parseEnvVarRef(*req.APIKey); envVar != "" {
			val, ok := os.LookupEnv(envVar)
			if !ok || val == "" {
				writeError(w, http.StatusBadRequest,
					fmt.Sprintf("environment variable %q is not set or empty", envVar))
				return
			}
			encrypted, encErr := crypto.Encrypt([]byte(val), h.server.masterKey)
			if encErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to encrypt API key")
				return
			}
			result, err := h.server.db.Pool.Exec(r.Context(), `
				UPDATE model_configs SET api_key_encrypted = $2, api_key_env_var = $3, updated_at = NOW()
				WHERE id = $1 AND org_id = $4
			`, cfgID, encrypted, envVar, claims.OrgID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if result.RowsAffected() == 0 {
				writeError(w, http.StatusNotFound, "model config not found")
				return
			}
		} else {
			encrypted, encErr := crypto.Encrypt([]byte(*req.APIKey), h.server.masterKey)
			if encErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to encrypt API key")
				return
			}
			result, err := h.server.db.Pool.Exec(r.Context(), `
				UPDATE model_configs SET api_key_encrypted = $2, api_key_env_var = NULL, updated_at = NOW()
				WHERE id = $1 AND org_id = $3
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
			price_per_input_token = COALESCE($8, price_per_input_token),
			price_per_output_token = COALESCE($9, price_per_output_token),
			price_per_cache_read_token = COALESCE($10, price_per_cache_read_token),
			updated_at = NOW()
		WHERE id = $1 AND org_id = $11
	`, cfgID, req.Name, req.Provider, req.BaseURL, req.Model, defaultParamsJSON, req.ContextWindow,
		req.PricePerInputToken, req.PricePerOutputToken, req.PricePerCacheReadToken, claims.OrgID)
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

// @Summary Delete a model config
// @Description Delete a model configuration
// @Tags model-configs
// @Param id path string true "Model Config ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /model-configs/{id} [delete]
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

// @Summary Test a model config
// @Description Test a model configuration by sending a test prompt
// @Tags model-configs
// @Produce json
// @Param id path string true "Model Config ID"
// @Success 200 {object} map[string]any
// @Failure 404 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Security BearerAuth
// @Router /model-configs/{id}/test [post]
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
		SELECT id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, price_per_input_token, price_per_output_token, price_per_cache_read_token, folder_id, created_by, created_at, updated_at, api_key_env_var
		FROM model_configs WHERE id = $1 AND org_id = $2
	`, cfgID, claims.OrgID).Scan(&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &apiKeyEncrypted, &defaultParams, &mc.ContextWindow, &mc.PricePerInputToken, &mc.PricePerOutputToken, &mc.PricePerCacheReadToken, &mc.FolderID, &mc.CreatedBy, &mc.CreatedAt, &mc.UpdatedAt, &mc.APIKeyEnvVar)
	if err != nil {
		writeError(w, http.StatusNotFound, "model config not found")
		return
	}

	if defaultParams != nil {
		json.Unmarshal(defaultParams, &mc.DefaultParams)
	}
	llmClient := agent.NewLLMClient(mc.BaseURL, mc.Model, apiKeyEncrypted, mc.DefaultParams)
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
