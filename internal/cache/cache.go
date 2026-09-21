package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	client *redis.Client
}

func New(redisURL string) (*Cache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("cache: parse URL: %w", err)
	}
	// Honor caller context deadlines on socket reads/writes. Without this the
	// driver uses only its configured Read/WriteTimeout, so a short publish or
	// cache deadline (e.g. the warehouse invalidation broadcast) would not
	// bound the operation.
	opts.ContextTimeoutEnabled = true
	return &Cache{client: redis.NewClient(opts)}, nil
}

func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Cache) Close() error {
	return c.client.Close()
}

func (c *Cache) Client() *redis.Client {
	return c.client
}
