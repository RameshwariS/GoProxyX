package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
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

// bucketResult is what one token-bucket check produces, regardless of
// whether the bucket is keyed by user id or by IP address.
type bucketResult struct {
	Allowed    bool
	Remaining  int64
	RetryAfter int64
}

// checkBucket runs the shared Lua script against one Redis key. Both
// RateLimitWithConfig (keyed by user) and RateLimitByIP (keyed by client
// address) call this, so the atomicity guarantee and the Redis-failure
// handling only need to be reasoned about once.
func checkBucket(r *http.Request, rdb *redis.Client, cfg RateLimitConfig, key string) (bucketResult, error) {
	if rdb == nil || cfg.Max <= 0 || cfg.Rate <= 0 {
		return bucketResult{}, fmt.Errorf("invalid rate limiter configuration: max=%d rate=%v", cfg.Max, cfg.Rate)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := tokenBucket.Run(ctx, rdb, []string{key}, cfg.Max, cfg.Rate, 1).Int64Slice()
	if err != nil {
		return bucketResult{}, err
	}
	if len(result) != 3 {
		return bucketResult{}, errors.New("unexpected result from rate limiter")
	}
	return bucketResult{Allowed: result[0] == 1, Remaining: result[1], RetryAfter: result[2]}, nil
}

// respondFromBucket writes the standard rate-limit headers and, if the
// bucket is empty, the 429 response. It returns whether the caller should
// stop (true) or continue to the next handler (false).
func respondFromBucket(w http.ResponseWriter, cfg RateLimitConfig, res bucketResult) (stop bool) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.Max))
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))
	if !res.Allowed {
		w.Header().Set("Retry-After", strconv.FormatInt(res.RetryAfter, 10))
		return true
	}
	return false
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

// RateLimitWithConfig creates a per-user token bucket middleware. It must
// run after Auth: it reads the verified user id from the request context,
// so it cannot protect routes nobody has logged into yet (see
// RateLimitByIP for those).
func RateLimitWithConfig(rdb *redis.Client, cfg RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := UserIDFrom(r.Context())
			if !ok {
				WriteError(w, http.StatusUnauthorized, "unauthenticated", "no user in context")
				return
			}

			res, err := checkBucket(r, rdb, cfg, "ratelimit:user:"+uid)
			if err != nil {
				slog.Error("rate limiter backend error", "err", err, "fail_open", cfg.FailOpen)
				if cfg.FailOpen {
					next.ServeHTTP(w, r)
					return
				}
				WriteError(w, http.StatusServiceUnavailable, "rate_limiter_unavailable", "try again shortly")
				return
			}

			if respondFromBucket(w, cfg, res) {
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extracts the caller's address for rate-limiting purposes. On
// Render (and behind most load balancers) the real client address arrives
// in X-Forwarded-For, with r.RemoteAddr instead holding the load balancer's
// own address -- keying on RemoteAddr there would put every visitor in one
// shared bucket. X-Forwarded-For can be a comma-separated chain if the
// request passed through several proxies; the first entry is the original
// client. This header is attacker-controlled on a request that reaches the
// gateway directly, but nothing here is reachable except through Render's
// own proxy, which sets it itself.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if ip, _, ok := strings.Cut(fwd, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(fwd)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RateLimitByIP protects routes that run before authentication exists yet
// -- chiefly POST /auth/login, which mints a JWT for anyone who calls it
// and so has no user id to key RateLimitWithConfig on. Without this,
// RateLimitWithConfig's protection is easy to route around entirely: mint
// unlimited tokens from /auth/login, one per request, and there is no
// per-user bucket to ever fill up. Fails open on a Redis error, the same
// policy as RateLimitWithConfig, so a Redis outage degrades to "login is
// temporarily unprotected" rather than "nobody can log in."
func RateLimitByIP(rdb *redis.Client, cfg RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res, err := checkBucket(r, rdb, cfg, "ratelimit:ip:"+clientIP(r))
			if err != nil {
				slog.Error("rate limiter backend error", "err", err, "fail_open", cfg.FailOpen)
				if cfg.FailOpen {
					next.ServeHTTP(w, r)
					return
				}
				WriteError(w, http.StatusServiceUnavailable, "rate_limiter_unavailable", "try again shortly")
				return
			}

			if respondFromBucket(w, cfg, res) {
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests -- slow down")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
