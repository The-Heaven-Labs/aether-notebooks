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
