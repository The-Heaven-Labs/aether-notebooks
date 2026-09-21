package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type rateLimitConfig struct {
	keyFunc func(r *http.Request) string
	limit   int
	window  time.Duration
}

// rateLimitScript increments the window counter and guarantees the key has an
// expiry. Setting the expiry only when the first INCR observes count 1 is not
// enough: if that request's context is canceled (or EXPIRE otherwise fails),
// the key survives without a TTL and the counter never resets, permanently
// rate-limiting that key. Repairing a missing TTL on any request lets a leaked
// key recover on its next hit while preserving the fixed window (later requests
// do not extend an existing expiry).
var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if redis.call('TTL', KEYS[1]) < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return count
`)

func (s *Server) rateLimit(cfg rateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.Cache == nil {
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("ratelimit:%s:%s", cfg.keyFunc(r), r.URL.Path)
			rdb := s.Cache.Client()

			// Detach from the request context: a canceled request must not
			// leave the counter without an expiry.
			count, err := rateLimitScript.Run(
				context.WithoutCancel(r.Context()),
				rdb, []string{key}, cfg.window.Milliseconds(),
			).Int64()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			remaining := cfg.limit - int(count)
			if remaining < 0 {
				remaining = 0
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if count > int64(cfg.limit) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
