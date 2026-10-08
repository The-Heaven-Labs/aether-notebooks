package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/models"
)

// cloudAPIBaseURL is the ClickHouse Cloud control-plane API root. It is a
// package variable so tests can point it at an httptest server. Production
// code never accepts a user-supplied URL, so there is no SSRF surface.
var cloudAPIBaseURL = "https://api.clickhouse.cloud"

// cloudStateHTTPClient bounds every control-plane read. The 5s timeout is far
// above normal latency but keeps a stuck upstream from holding a request open.
var cloudStateHTTPClient = &http.Client{Timeout: cloudStateRequestTimeout}

const (
	// cloudStateRequestTimeout caps one control-plane call.
	cloudStateRequestTimeout = 5 * time.Second
	// cloudStateCacheTTL bounds how stale a served state can be. Control-plane
	// reads never wake the service, so a short cache is about API traffic, not
	// correctness.
	cloudStateCacheTTL = 45 * time.Second
	// cloudStateResponseMaxBytes caps how much of an upstream response body is
	// read (the service resource is a few KB).
	cloudStateResponseMaxBytes = 1 << 20
)

// cloudStateCredentials are the optional per-connector ClickHouse Cloud API
// credentials. The zero value means "not configured".
type cloudStateCredentials struct {
	OrgID     string
	ServiceID string
	KeyID     string
	KeySecret string
}

// configured reports whether all four credential fields are present.
func (c cloudStateCredentials) configured() bool {
	return c.OrgID != "" && c.ServiceID != "" && c.KeyID != "" && c.KeySecret != ""
}

// cloudServiceState is the subset of the ClickHouse Cloud service resource the
// status chip needs; the JSON tags mirror the control-plane field names.
type cloudServiceState struct {
	State              string  `json:"state"`
	IdleScaling        bool    `json:"idleScaling"`
	IdleTimeoutMinutes float64 `json:"idleTimeoutMinutes"`
}

// cloudStateResult pairs a fetched state with the time it was fetched, so the
// cache can age it out.
type cloudStateResult struct {
	state     cloudServiceState
	checkedAt time.Time
}

// cloudCredentialsFromConfig extracts Cloud API credentials from the decrypted
// connector config. Malformed JSON degrades to "not configured" instead of
// failing the request.
func cloudCredentialsFromConfig(raw []byte) cloudStateCredentials {
	var cfg struct {
		CloudOrgID     string `json:"cloud_org_id"`
		CloudServiceID string `json:"cloud_service_id"`
		CloudKeyID     string `json:"cloud_key_id"`
		CloudKeySecret string `json:"cloud_key_secret"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cloudStateCredentials{}
	}
	return cloudStateCredentials{
		OrgID:     strings.TrimSpace(cfg.CloudOrgID),
		ServiceID: strings.TrimSpace(cfg.CloudServiceID),
		KeyID:     strings.TrimSpace(cfg.CloudKeyID),
		KeySecret: cfg.CloudKeySecret,
	}
}

// cloudStateCache is a small per-connector TTL cache for successful
// control-plane reads. Errors are deliberately not cached so a credential fix
// is visible on the next poll.
type cloudStateCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cloudStateResult
}

func newCloudStateCache(ttl time.Duration) *cloudStateCache {
	return &cloudStateCache{ttl: ttl, entries: make(map[string]cloudStateResult)}
}

func (c *cloudStateCache) get(connectorID string) (cloudStateResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[connectorID]
	if !ok {
		return cloudStateResult{}, false
	}
	if time.Since(entry.checkedAt) > c.ttl {
		delete(c.entries, connectorID)
		return cloudStateResult{}, false
	}
	return entry, true
}

func (c *cloudStateCache) put(connectorID string, result cloudStateResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[connectorID] = result
}

// cloudStateFlight dedupes concurrent control-plane reads for the same
// connector. The cache in front handles the steady state; this handles bursts
// (e.g. several page loads in one polling window).
var cloudStateFlight singleflight.Group

// fetchCloudServiceState reads one service from the ClickHouse Cloud
// control-plane API. It authenticates with the API key pair and never wakes
// the service. Errors carry no credential material: they are built from
// transport and status information only.
func fetchCloudServiceState(ctx context.Context, creds cloudStateCredentials) (cloudServiceState, error) {
	ctx, cancel := context.WithTimeout(ctx, cloudStateRequestTimeout)
	defer cancel()

	endpoint := fmt.Sprintf("%s/v1/organizations/%s/services/%s",
		strings.TrimRight(cloudAPIBaseURL, "/"),
		url.PathEscape(creds.OrgID), url.PathEscape(creds.ServiceID),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return cloudServiceState{}, fmt.Errorf("build clickhouse cloud request: %w", err)
	}
	req.SetBasicAuth(creds.KeyID, creds.KeySecret)

	resp, err := cloudStateHTTPClient.Do(req)
	if err != nil {
		return cloudServiceState{}, fmt.Errorf("clickhouse cloud request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return cloudServiceState{}, fmt.Errorf("clickhouse cloud API returned HTTP %d", resp.StatusCode)
	}

	var body struct {
		Result cloudServiceState `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, cloudStateResponseMaxBytes)).Decode(&body); err != nil {
		return cloudServiceState{}, fmt.Errorf("invalid clickhouse cloud response: %w", err)
	}
	if strings.TrimSpace(body.Result.State) == "" {
		return cloudServiceState{}, fmt.Errorf("clickhouse cloud response carried no service state")
	}
	return body.Result, nil
}

// cloudStateResponse is the wire shape for GET /connectors/{id}/cloud-state.
// `configured` separates "no Cloud API credentials" from "configured but the
// read failed"; failures stay HTTP 200 so the page degrades into the inferred
// state instead of showing a request error.
type cloudStateResponse struct {
	Configured         bool     `json:"configured"`
	State              string   `json:"state,omitempty"`
	IdleScaling        *bool    `json:"idle_scaling,omitempty"`
	IdleTimeoutMinutes *float64 `json:"idle_timeout_minutes,omitempty"`
	CheckedAt          string   `json:"checked_at,omitempty"`
	Error              string   `json:"error,omitempty"`
}

// @Summary Get ClickHouse Cloud service state
// @Description Returns the ClickHouse Cloud control-plane state for a ClickHouse connector with Cloud API credentials. Non-ClickHouse connectors and connectors without credentials return 200 with {"configured": false}; upstream failures return 200 with an error field. Reading state never wakes an idle service.
// @Tags connectors
// @Produce json
// @Param id path string true "Connector ID"
// @Success 200 {object} map[string]interface{}
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id}/cloud-state [get]
func (s *Server) handleConnectorCloudState(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	connID := r.PathValue("id")
	ctx := r.Context()

	connType, configEnc, err := s.loadConnectorRow(ctx, connID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	if connType != models.ConnectorClickHouse {
		writeJSON(w, http.StatusOK, cloudStateResponse{Configured: false})
		return
	}

	plain, err := crypto.Decrypt(configEnc, s.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load connector config")
		return
	}
	creds := cloudCredentialsFromConfig(plain)
	if !creds.configured() {
		writeJSON(w, http.StatusOK, cloudStateResponse{Configured: false})
		return
	}

	result, err := s.cachedCloudServiceState(ctx, connID, creds)
	if err != nil {
		// The upstream error carries no credential material: fetchCloudServiceState
		// builds messages from transport and status information only.
		writeJSON(w, http.StatusOK, cloudStateResponse{Configured: true, Error: err.Error()})
		return
	}
	idleScaling := result.state.IdleScaling
	idleTimeout := result.state.IdleTimeoutMinutes
	writeJSON(w, http.StatusOK, cloudStateResponse{
		Configured:         true,
		State:              result.state.State,
		IdleScaling:        &idleScaling,
		IdleTimeoutMinutes: &idleTimeout,
		CheckedAt:          result.checkedAt.UTC().Format(time.RFC3339),
	})
}

// cachedCloudServiceState serves the in-memory TTL cache, falling back to one
// deduplicated control-plane read per connector.
func (s *Server) cachedCloudServiceState(ctx context.Context, connID string, creds cloudStateCredentials) (cloudStateResult, error) {
	if result, ok := s.cloudStateCache.get(connID); ok {
		return result, nil
	}
	value, err, _ := cloudStateFlight.Do(connID, func() (any, error) {
		// The flight window may have filled the cache while this call waited.
		if result, ok := s.cloudStateCache.get(connID); ok {
			return result, nil
		}
		state, err := fetchCloudServiceState(ctx, creds)
		if err != nil {
			return cloudStateResult{}, err
		}
		result := cloudStateResult{state: state, checkedAt: time.Now()}
		s.cloudStateCache.put(connID, result)
		return result, nil
	})
	if err != nil {
		return cloudStateResult{}, err
	}
	return value.(cloudStateResult), nil
}
