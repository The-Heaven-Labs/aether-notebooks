package cache_test

import (
	"context"
	"testing"

	"github.com/the-heaven-labs/aether/internal/cache"
)

func TestCacheNew(t *testing.T) {
	c, err := cache.New("redis://localhost:6379")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestCacheClientNotNil(t *testing.T) {
	c, err := cache.New("redis://localhost:6379")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if c.Client() == nil {
		t.Fatal("Client() returned nil")
	}
}
