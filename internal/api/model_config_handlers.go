package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/headers"
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
			   api_key_env_var, custom_headers, custom_header_secrets
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
		var defaultParams, customHeaders []byte
		var customHeaderSecrets []string
		rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Provider, &c.BaseURL, &c.Model, &c.APIKeyEncrypted,
			&defaultParams, &c.ContextWindow, &c.PricePerInputToken, &c.PricePerOutputToken,
			&c.PricePerCacheReadToken, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
			&c.APIKeyEnvVar, &customHeaders, &customHeaderSecrets)
		json.Unmarshal(defaultParams, &c.DefaultParams)
		if customHeaders != nil {
			json.Unmarshal(customHeaders, &c.CustomHeaders)
		}
		if c.CustomHeaders == nil {
			c.CustomHeaders = models.JSONMap{}
		}
		c.CustomHeaderSecrets = customHeaderSecrets
		headers.RedactSecrets(c.CustomHeaders, c.CustomHeaderSecrets)
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
	var defaultParams, customHeaders []byte
	var customHeaderSecrets []string
	err = h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, org_id, name, provider, base_url, model, api_key_encrypted,
			   default_params, context_window, price_per_input_token, price_per_output_token,
			   price_per_cache_read_token, folder_id, created_by, created_at, updated_at,
			   api_key_env_var, custom_headers, custom_header_secrets
		FROM model_configs WHERE id = $1 AND org_id = $2
	`, id, claims.OrgID).Scan(&c.ID, &c.OrgID, &c.Name, &c.Provider, &c.BaseURL, &c.Model, &apiKeyEncrypted,
		&defaultParams, &c.ContextWindow, &c.PricePerInputToken, &c.PricePerOutputToken,
		&c.PricePerCacheReadToken, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		&c.APIKeyEnvVar, &customHeaders, &customHeaderSecrets)
	if err != nil {
		writeError(w, http.StatusNotFound, "model config not found")
		return
	}
	json.Unmarshal(defaultParams, &c.DefaultParams)
	if customHeaders != nil {
		json.Unmarshal(customHeaders, &c.CustomHeaders)
	}
	if c.CustomHeaders == nil {
		c.CustomHeaders = models.JSONMap{}
	}
	c.CustomHeaderSecrets = customHeaderSecrets
	headers.RedactSecrets(c.CustomHeaders, c.CustomHeaderSecrets)
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
		CustomHeaders          models.JSONMap `json:"custom_headers"`
		CustomHeaderSecrets    []string       `json:"custom_header_secrets"`
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

	if err := headers.Validate(req.CustomHeaders, req.CustomHeaderSecrets); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	encryptedHeaders, encErr := headers.EncryptSecrets(req.CustomHeaders, req.CustomHeaderSecrets, h.server.masterKey)
	if encErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt custom headers")
		return
	}
	customHeadersJSON, _ := json.Marshal(encryptedHeaders)
	// custom_header_secrets is NOT NULL: a missing list must write an empty
	// array rather than SQL NULL.
	customHeaderSecrets := req.CustomHeaderSecrets
	if customHeaderSecrets == nil {
		customHeaderSecrets = []string{}
	}

	_, err = h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted,
			default_params, context_window, price_per_input_token, price_per_output_token,
			price_per_cache_read_token, folder_id, created_by, created_at, updated_at,
			api_key_env_var, custom_headers, custom_header_secrets)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW(), NOW(), $15, $16, $17)
	`, cfgID, claims.OrgID, req.Name, req.Provider, req.BaseURL, req.Model, encryptedKey,
		defaultParamsJSON, req.ContextWindow, req.PricePerInputToken, req.PricePerOutputToken,
		req.PricePerCacheReadToken, req.FolderID, claims.UserID, apiKeyEnvVar,
		customHeadersJSON, customHeaderSecrets)
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
		CustomHeaders          *models.JSONMap `json:"custom_headers"`
		CustomHeaderSecrets    *[]string       `json:"custom_header_secrets"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Custom headers are validated and prepared before any write: a rejected
	// update must not partially apply (e.g. replace the API key first).
	var customHeadersJSON []byte
	var customHeaderSecrets []string
	if req.CustomHeaders != nil {
		secrets := []string{}
		if req.CustomHeaderSecrets != nil {
			secrets = *req.CustomHeaderSecrets
		}
		if err := headers.Validate(*req.CustomHeaders, secrets); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Sentinel values mean "keep the stored secret". A failed read must
		// surface so a sentinel can never be persisted literally.
		var old models.JSONMap
		var oldRaw []byte
		readErr := h.server.db.Pool.QueryRow(r.Context(),
			`SELECT custom_headers FROM model_configs WHERE id = $1 AND org_id = $2`,
			cfgID, claims.OrgID).Scan(&oldRaw)
		if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to load model config")
			return
		}
		if readErr == nil && len(oldRaw) > 0 {
			// A stored map that cannot be decoded is treated as absent, so its
			// sentinels are dropped rather than persisted literally.
			var parsed models.JSONMap
			if err := json.Unmarshal(oldRaw, &parsed); err == nil {
				old = parsed
			}
		}
		encrypted, kept, encErr := headers.EncryptUpdate(*req.CustomHeaders, secrets, old, h.server.masterKey)
		if encErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt custom headers")
			return
		}
		customHeadersJSON, _ = json.Marshal(encrypted)
		customHeaderSecrets = kept
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
			custom_headers = COALESCE($11, custom_headers),
			custom_header_secrets = COALESCE($12, custom_header_secrets),
			updated_at = NOW()
		WHERE id = $1 AND org_id = $13
	`, cfgID, req.Name, req.Provider, req.BaseURL, req.Model, defaultParamsJSON, req.ContextWindow,
		req.PricePerInputToken, req.PricePerOutputToken, req.PricePerCacheReadToken,
		customHeadersJSON, customHeaderSecrets, claims.OrgID)
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
	var customHeadersRaw []byte
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, price_per_input_token, price_per_output_token, price_per_cache_read_token, folder_id, created_by, created_at, updated_at, api_key_env_var, custom_headers, custom_header_secrets
		FROM model_configs WHERE id = $1 AND org_id = $2
	`, cfgID, claims.OrgID).Scan(&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &apiKeyEncrypted, &defaultParams, &mc.ContextWindow, &mc.PricePerInputToken, &mc.PricePerOutputToken, &mc.PricePerCacheReadToken, &mc.FolderID, &mc.CreatedBy, &mc.CreatedAt, &mc.UpdatedAt, &mc.APIKeyEnvVar, &customHeadersRaw, &mc.CustomHeaderSecrets)
	if err != nil {
		writeError(w, http.StatusNotFound, "model config not found")
		return
	}

	if defaultParams != nil {
		json.Unmarshal(defaultParams, &mc.DefaultParams)
	}
	if customHeadersRaw != nil {
		json.Unmarshal(customHeadersRaw, &mc.CustomHeaders)
	}
	llmClient := agent.NewLLMClient(mc.BaseURL, mc.Model, apiKeyEncrypted, mc.DefaultParams, mc.CustomHeaders, mc.CustomHeaderSecrets)
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
