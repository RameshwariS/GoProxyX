package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// data in redis ->
// KEYS[1]  → the Redis key  (e.g. "ratelimit:user:u1")
// ARGV[1]  → current time   (e.g. 1720000000.523)
// ARGV[2]  → max tokens     (e.g. 10)
// ARGV[3]  → refill rate    (e.g. 2.0  meaning 2 tokens per second)

// tokenBucketLuaScript atomically refills and consumes a token bucket. Redis
// supplies the clock so requests handled by different gateway instances use
// the same time source.
const tokenBucketLuaScript = `
local key = KEYS[1]
local max_tokens = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])

local t = redis.call('TIME')
local now = tonumber(t[1]) + tonumber(t[2]) / 1000000

local data = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(data[1])
local last_refill = tonumber(data[2])
if tokens == nil then
  tokens = max_tokens
end
if last_refill == nil then
  last_refill = now
end

tokens = math.min(max_tokens, tokens + math.max(0, now - last_refill) * refill_rate)

local allowed = 0
local retry_after = 0
if tokens >= cost then
  tokens = tokens - cost
  allowed = 1
else
  retry_after = math.ceil((cost - tokens) / refill_rate)
end

redis.call('HSET', key, 'tokens', tokens, 'last_refill', now)
redis.call('EXPIRE', key, math.ceil(max_tokens / refill_rate) * 2)
return {allowed, math.floor(tokens), retry_after}
`

var tokenBucket = redis.NewScript(tokenBucketLuaScript)

// RateLimitConfig controls the token bucket and Redis failure behavior.
type RateLimitConfig struct {
	Max      int
	Rate     float64
	Timeout  time.Duration
	FailOpen bool
}

// RateLimit preserves the original API and allows requests through if Redis
// is unavailable, matching the middleware's previous behavior.
func RateLimit(rdb *redis.Client, maxTokens int, refillRate float64) func(http.Handler) http.Handler {
	return RateLimitWithConfig(rdb, RateLimitConfig{
		Max:      maxTokens,
		Rate:     refillRate,
		FailOpen: true,
	})
}

// RateLimitWithConfig creates a per-user token bucket middleware.
func RateLimitWithConfig(rdb *redis.Client, cfg RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := UserIDFrom(r.Context())
			if !ok {
				WriteError(w, http.StatusUnauthorized, "unauthenticated", "no user in context")
				return
			}

			if rdb == nil || cfg.Max <= 0 || cfg.Rate <= 0 {
				slog.Error("invalid rate limiter configuration", "max", cfg.Max, "rate", cfg.Rate)
				WriteError(w, http.StatusServiceUnavailable, "rate_limiter_unavailable", "try again shortly")
				return
			}

			timeout := cfg.Timeout
			if timeout <= 0 {
				timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			result, err := tokenBucket.Run(ctx, rdb, []string{"ratelimit:user:" + uid}, cfg.Max, cfg.Rate, 1).Int64Slice()
			if err != nil || len(result) != 3 {
				if err == nil {
					err = errors.New("unexpected result from rate limiter")
				}
				slog.Error("rate limiter backend error", "err", err, "fail_open", cfg.FailOpen)
				if cfg.FailOpen {
					next.ServeHTTP(w, r)
					return
				}
				WriteError(w, http.StatusServiceUnavailable, "rate_limiter_unavailable", "try again shortly")
				return
			}

			allowed, remaining, retryAfter := result[0], result[1], result[2]
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.Max))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
			if allowed == 0 {
				w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
