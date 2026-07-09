package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type rateLimitConfig struct {
	keyFunc func(r *http.Request) string
	limit   int
	window  time.Duration
}

func (s *Server) rateLimit(cfg rateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.Cache == nil {
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("ratelimit:%s:%s", cfg.keyFunc(r), r.URL.Path)
			rdb := s.Cache.Client()

			count, err := rdb.Incr(r.Context(), key).Result()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if count == 1 {
				rdb.Expire(r.Context(), key, cfg.window)
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
