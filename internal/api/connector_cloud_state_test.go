package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloudCredentialsFromConfig(t *testing.T) {
	creds := cloudCredentialsFromConfig([]byte(`{"cloud_org_id":" org ","cloud_service_id":"svc","cloud_key_id":"key","cloud_key_secret":"secret"}`))
	require.True(t, creds.configured())
	require.Equal(t, "org", creds.OrgID)

	require.False(t, cloudCredentialsFromConfig([]byte(`{"cloud_org_id":"org"}`)).configured())
	require.False(t, cloudCredentialsFromConfig([]byte(`not-json`)).configured())
}

func TestCloudStateCacheExpiry(t *testing.T) {
	cache := newCloudStateCache(25 * time.Millisecond)
	cache.put("conn-a", cloudStateResult{state: cloudServiceState{State: "running"}, checkedAt: time.Now()})
	if _, ok := cache.get("conn-a"); !ok {
		t.Fatal("expected a fresh cache hit")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := cache.get("conn-a"); ok {
		t.Fatal("expected the entry to expire")
	}
}
