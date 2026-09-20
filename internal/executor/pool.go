package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/the-heaven-labs/aether/internal/models"
)

// PoolConfig configures a ConnPool.
type PoolConfig struct {
	// MaxPools caps the number of pooled connections per pool. <= 0 means
	// unlimited. In-use entries are never evicted, so the pool may temporarily
	// exceed MaxPools while connections are leased; the overage is resolved
	// when leases are released.
	MaxPools int
	// IdleTTL closes connections that have been idle for at least this long.
	// <= 0 disables idle eviction. In-use entries are skipped.
	IdleTTL time.Duration
	// Open opens a new connection for a connector config. When nil the default
	// opener is used, which builds driver options via chOptions and does not
	// dial (dial/auth errors surface on first use). Open is called while the
	// pool mutex is held, so it must not block or call back into the pool.
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
	refs     int
	dead     bool
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

// Get returns a pooled connection for (endpoint, user) and a release function
// that must be called exactly once when the connection is no longer in use.
// Connection-affecting config is fingerprinted; a change closes the stale
// connection and opens a new one.
func (p *ConnPool) Get(endpoint, user string, cfg models.ConnectorConfig) (clickhouse.Conn, func(), error) {
	key := poolKey{endpoint: endpoint, user: user}
	fp := credentialFingerprint(cfg)

	p.mu.Lock()
	defer p.mu.Unlock()

	if e, ok := p.entries[key]; ok {
		if e.fp == fp {
			p.touch(key)
			e.lastUsed = time.Now()
			e.refs++
			return e.conn, p.releaser(e), nil
		}
		p.detachLocked(key)
	}

	conn, err := p.cfg.Open(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("open clickhouse connection for %q at %q: %w", user, endpoint, err)
	}

	e := &poolEntry{conn: conn, lastUsed: time.Now(), cfg: cfg, fp: fp, refs: 1}
	p.entries[key] = e
	p.keys = append(p.keys, key)
	p.evictLocked()
	return conn, p.releaser(e), nil
}

// releaser returns an idempotent release function for a single acquisition.
func (p *ConnPool) releaser(e *poolEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.releaseLocked(e)
		})
	}
}

func (p *ConnPool) releaseLocked(e *poolEntry) {
	if e.refs > 0 {
		e.refs--
	}
	if e.refs > 0 {
		return
	}
	if e.dead {
		_ = e.conn.Close()
		return
	}
	p.evictLocked()
}

// Invalidate removes the pooled connection for (endpoint, user) so new Gets
// open a fresh connection. An in-use connection is closed by its last release.
func (p *ConnPool) Invalidate(endpoint, user string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.detachLocked(poolKey{endpoint: endpoint, user: user})
}

// CloseIdle evicts connections idle for at least IdleTTL as of now. In-use
// connections are skipped.
func (p *ConnPool) CloseIdle(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg.IdleTTL <= 0 {
		return
	}
	for _, key := range append([]poolKey(nil), p.keys...) {
		e, ok := p.entries[key]
		if !ok || e.refs > 0 {
			continue
		}
		if now.Sub(e.lastUsed) >= p.cfg.IdleTTL {
			p.detachLocked(key)
		}
	}
}

// CloseAll detaches and closes every pooled connection and clears the pool.
// In-use connections are not closed mid-query; they are marked dead and closed
// by their last release.
func (p *ConnPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, key := range append([]poolKey(nil), p.keys...) {
		p.detachLocked(key)
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

// detachLocked removes an entry from the pool. If it has no outstanding leases
// it is closed immediately; otherwise it is marked dead and closed by its last
// release.
func (p *ConnPool) detachLocked(key poolKey) {
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
	if e.refs == 0 {
		_ = e.conn.Close()
		return
	}
	e.dead = true
}

// evictLocked trims the pool to MaxPools by detaching least-recently-used idle
// entries. When every entry is in use the pool is left above MaxPools until a
// release makes eviction possible.
func (p *ConnPool) evictLocked() {
	if p.cfg.MaxPools <= 0 {
		return
	}
	for len(p.entries) > p.cfg.MaxPools {
		evicted := false
		for _, key := range p.keys {
			if e := p.entries[key]; e != nil && e.refs == 0 {
				p.detachLocked(key)
				evicted = true
				break
			}
		}
		if !evicted {
			return
		}
	}
}

// credentialFingerprint hashes every connection-affecting config field so a
// credential or target change reopens the pooled connection. Fields are
// length-prefixed so values cannot collide across field boundaries. The user
// is also part of the pool key; it is included here for completeness.
func credentialFingerprint(cfg models.ConnectorConfig) string {
	var b strings.Builder
	fields := []string{
		cfg.Host,
		strconv.Itoa(cfg.Port),
		cfg.User,
		cfg.Password,
		cfg.Database,
		cfg.SSLMode,
	}
	for _, f := range fields {
		b.WriteString(strconv.Itoa(len(f)))
		b.WriteByte(':')
		b.WriteString(f)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
