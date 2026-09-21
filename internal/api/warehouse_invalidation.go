package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"time"
)

// defaultWarehouseInvalidationChannel is the Redis pub/sub channel carrying
// pooled-identity invalidation notices between replicas. The connPool that
// serves per-user ClickHouse executions is process-local, so a reconcile (or a
// warehouse delete) on one replica must broadcast the affected identity names
// so every other replica drops its resident sessions too. Servers override
// Server.warehouseInvalidationChannel for isolation in tests.
const defaultWarehouseInvalidationChannel = "aether:warehouse-identity-invalidation"

// warehouseIdentityInvalidationChunkSize bounds how many identity names travel
// in one pub/sub message. A large org can affect more identities than a single
// Redis message should carry, so sets are split into chunks of at most this
// size.
const warehouseIdentityInvalidationChunkSize = 1000

// warehouseIdentityInvalidationPublishTimeout bounds one publish attempt.
// Publishing is additive on top of the local invalidation, so a Redis outage
// must fail open quickly instead of blocking the request or reconcile path.
const warehouseIdentityInvalidationPublishTimeout = time.Second

// warehouseIdentityInvalidationMessage is the JSON body of one invalidation
// notice. The field (rather than a bare array) leaves room for growth without
// breaking a rolling deploy.
type warehouseIdentityInvalidationMessage struct {
	Users []string `json:"users"`
}

// warehouseInvalidationChannelName returns the channel this server broadcasts
// and subscribes on. The zero value falls back to the default so a Server
// built without NewServer still works.
func (s *Server) warehouseInvalidationChannelName() string {
	if s.warehouseInvalidationChannel != "" {
		return s.warehouseInvalidationChannel
	}
	return defaultWarehouseInvalidationChannel
}

// publishWarehouseIdentityInvalidation broadcasts the affected identity names
// to every replica, deduplicated and split into at most
// warehouseIdentityInvalidationChunkSize names per message. It is best-effort:
// the caller has already invalidated its own pool, so a Redis failure is
// logged and swallowed and never fails the reconcile, the fail-closed path, or
// the warehouse delete. A nil Redis client (tests, no Redis configured) skips
// the broadcast entirely.
func (s *Server) publishWarehouseIdentityInvalidation(users map[string]struct{}) {
	if s.rdb == nil || len(users) == 0 {
		return
	}
	channel := s.warehouseInvalidationChannelName()
	names := make([]string, 0, len(users))
	for name := range users {
		names = append(names, name)
	}
	// Deterministic order keeps chunks stable across replicas and makes
	// chunking observable in tests.
	sort.Strings(names)

	for start := 0; start < len(names); start += warehouseIdentityInvalidationChunkSize {
		end := start + warehouseIdentityInvalidationChunkSize
		if end > len(names) {
			end = len(names)
		}
		payload, err := json.Marshal(warehouseIdentityInvalidationMessage{Users: names[start:end]})
		if err != nil {
			slog.Warn("warehouse identity invalidation marshal failed",
				"error", err, "users", end-start)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), warehouseIdentityInvalidationPublishTimeout)
		err = s.rdb.Publish(ctx, channel, payload).Err()
		cancel()
		if err != nil {
			slog.Warn("warehouse identity invalidation publish failed",
				"error", err, "users", end-start)
			return
		}
	}
}

// startWarehouseInvalidationSubscriber starts the replica's subscriber loop.
// It is a no-op without a Redis client, so tests and single-node setups
// without Redis keep the local-only invalidation path. The loop stops when ctx
// is cancelled or Server.Close runs.
func (s *Server) startWarehouseInvalidationSubscriber(ctx context.Context) {
	if s.rdb == nil {
		return
	}
	s.warehouseInvalidationLoop.start(ctx, func(loopCtx context.Context) {
		s.runWarehouseInvalidationSubscriber(loopCtx)
	})
}

// runWarehouseInvalidationSubscriber subscribes to the invalidation channel
// and applies every well-formed message to the local pool. It mirrors the WS
// hub's lifecycle: on a closed channel it closes the subscription, backs off
// briefly, and resubscribes, exiting promptly on cancellation. Malformed
// payloads are logged and skipped; an unparseable message must never panic or
// invalidate an arbitrary (or empty) set.
func (s *Server) runWarehouseInvalidationSubscriber(ctx context.Context) {
	channel := s.warehouseInvalidationChannelName()
	for {
		pubsub := s.rdb.Subscribe(ctx, channel)
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				pubsub.Close()
				return
			case msg := <-ch:
				if msg == nil {
					goto reconnect
				}
				s.applyWarehouseIdentityInvalidation([]byte(msg.Payload))
			}
		}
	reconnect:
		pubsub.Close()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// applyWarehouseIdentityInvalidation applies one broadcast payload to the
// local pool. Invalid JSON is logged and ignored.
func (s *Server) applyWarehouseIdentityInvalidation(payload []byte) {
	var msg warehouseIdentityInvalidationMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		slog.Warn("warehouse identity invalidation: malformed payload ignored", "error", err)
		return
	}
	if len(msg.Users) == 0 || s.connPool == nil {
		return
	}
	users := make(map[string]struct{}, len(msg.Users))
	for _, name := range msg.Users {
		if name != "" {
			users[name] = struct{}{}
		}
	}
	if len(users) > 0 {
		s.connPool.InvalidateUsers(users)
	}
}
