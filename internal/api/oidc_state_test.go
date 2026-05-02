package api_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestOIDCStateRedisRoundTrip verifies that OAuth2 state issued on one
// server instance can be consumed by a second instance sharing the same Redis.
func TestOIDCStateRedisRoundTrip(t *testing.T) {
	// Two separate server instances sharing one Redis (simulates multi-pod)
	s1 := setupTestServer(t)
	s2 := setupTestServer(t)

	ctx := context.Background()
	token := fmt.Sprintf("test-state-token-%d", time.Now().UnixNano())
	key := fmt.Sprintf("oidc:state:%s", token)
	payload := "some-provider-id"

	// Clean up in case a previous run left the key
	s1.Cache.Client().Del(ctx, key)

	// s1 issues the state token
	ok, err := s1.Cache.Client().SetNX(ctx, key, payload, 10*time.Minute).Result()
	if err != nil {
		t.Fatalf("SetNX: %v", err)
	}
	if !ok {
		t.Fatal("SetNX returned false — key already exists")
	}

	// s2 consumes it (simulates callback hitting a different pod)
	got, err := s2.Cache.Client().GetDel(ctx, key).Result()
	if err != nil {
		t.Fatalf("GetDel: %v", err)
	}
	if got != payload {
		t.Errorf("expected %q, got %q", payload, got)
	}

	// Consuming again must fail (no replay)
	_, err = s1.Cache.Client().GetDel(ctx, key).Result()
	if err == nil {
		t.Error("expected error on second GetDel (replay), got nil")
	}
	if err != nil && err != redis.Nil {
		t.Errorf("expected redis.Nil on replay, got: %v", err)
	}
}
