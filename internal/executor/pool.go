package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/the-heaven-labs/aether/internal/models"
)

// PoolConfig configures a ConnPool.
type PoolConfig struct {
	// MaxPools caps the number of pooled connections per pool. <= 0 means
	// unlimited.
	MaxPools int
	// IdleTTL closes connections that have been idle for at least this long.
	// <= 0 disables idle eviction.
	IdleTTL time.Duration
	// PerUserMaxOpen is reserved for per-user connection limits; the pool
	// currently keeps at most one connection per (endpoint, user).
	PerUserMaxOpen int
	// Open opens a new connection for a connector config. When nil the default
	// opener is used, which builds driver options via chOptions and does not
	// dial (dial/auth errors surface on first use).
	Open func(cfg models.ConnectorConfig) (clickhouse.Conn, error)
}

type poolKey struct {
	endpoint string
	user     string
}

type poolEntry struct {
	conn     clickhouse.Conn
	lastUsed time.Time
	cfg      models.ConnectorConfig
	fp       string
}

// ConnPool is a process-local pool of ClickHouse connections keyed by
// (endpoint, user). It is safe for concurrent use.
type ConnPool struct {
	mu      sync.Mutex
	cfg     PoolConfig
	entries map[poolKey]*poolEntry
	keys    []poolKey // LRU order, most recently used last
}

// NewConnPool creates a pool. It never opens a connection eagerly.
func NewConnPool(cfg PoolConfig) *ConnPool {
	if cfg.Open == nil {
		cfg.Open = defaultPoolOpen
	}
	return &ConnPool{
		cfg:     cfg,
		entries: make(map[poolKey]*poolEntry),
	}
}

func defaultPoolOpen(cfg models.ConnectorConfig) (clickhouse.Conn, error) {
	conn, err := clickhouse.Open(chOptions(cfg))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return conn, nil
}

// Get returns a pooled connection for (endpoint, user), opening one if needed.
// Credentials are fingerprinted; a changed password closes the stale
// connection and opens a new one.
func (p *ConnPool) Get(endpoint, user string, cfg models.ConnectorConfig) (clickhouse.Conn, error) {
	key := poolKey{endpoint: endpoint, user: user}
	fp := credentialFingerprint(cfg.Password)

	p.mu.Lock()
	defer p.mu.Unlock()

	if e, ok := p.entries[key]; ok {
		if e.fp == fp {
			p.touch(key)
			e.lastUsed = time.Now()
			return e.conn, nil
		}
		p.remove(key, true)
	}

	conn, err := p.cfg.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse connection for %q at %q: %w", user, endpoint, err)
	}

	p.entries[key] = &poolEntry{conn: conn, lastUsed: time.Now(), cfg: cfg, fp: fp}
	p.keys = append(p.keys, key)
	p.evictLocked()
	return conn, nil
}

// Invalidate closes and removes the pooled connection for (endpoint, user).
func (p *ConnPool) Invalidate(endpoint, user string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.remove(poolKey{endpoint: endpoint, user: user}, true)
}

// CloseIdle evicts connections idle for at least IdleTTL as of now.
func (p *ConnPool) CloseIdle(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg.IdleTTL <= 0 {
		return
	}
	for _, key := range append([]poolKey(nil), p.keys...) {
		e, ok := p.entries[key]
		if !ok {
			continue
		}
		if now.Sub(e.lastUsed) >= p.cfg.IdleTTL {
			p.remove(key, true)
		}
	}
}

// Len returns the number of pooled connections.
func (p *ConnPool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

func (p *ConnPool) touch(key poolKey) {
	for i, k := range p.keys {
		if k == key {
			p.keys = append(p.keys[:i], p.keys[i+1:]...)
			break
		}
	}
	p.keys = append(p.keys, key)
}

func (p *ConnPool) remove(key poolKey, closeConn bool) {
	e, ok := p.entries[key]
	if !ok {
		return
	}
	delete(p.entries, key)
	for i, k := range p.keys {
		if k == key {
			p.keys = append(p.keys[:i], p.keys[i+1:]...)
			break
		}
	}
	if closeConn {
		_ = e.conn.Close()
	}
}

func (p *ConnPool) evictLocked() {
	if p.cfg.MaxPools <= 0 {
		return
	}
	for len(p.entries) > p.cfg.MaxPools && len(p.keys) > 0 {
		p.remove(p.keys[0], true)
	}
}

func credentialFingerprint(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}
