package middleware

import (
	"context"
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

const tokenBucketLuaScript = `
-- reading from current state
local key = KEYS[1]
local curr_time = ARGV[1]
local maxTokens = ARGV[2]
local refillRate = ARGV[3]
-- reading from redis
local data = redis.call("HMGET", key, "tokens", "last_refill")  -- command,arg1(variable),arg2(fixed),arg3
local tokens = tonumber(data[1])
local lastRefill = tonumber(data[2])
if tokens == nil then
	tokens = tonumber(maxTokens) - 1
	redis.call("HMSET",key,"tokens",tokens,"last_refill",curr_time)
	redis.call("EXPIRE",key,3600)
	return {1,tokens} --  1 means allowed 
end
-- if tokens != nil that means key exist and have to allocate the remaining bucket
local elapsed = curr_time - lastRefill
local refill = elapsed * tonumber(refillRate)
tokens = math.min(tonumber(maxTokens),tokens+refill)

if tokens < 1 then
-- to save the partial tokens like if tokens = 0.5 then we will save it in redis
	redis.call("HMSET",key,"tokens",tokens,"last_refill",curr_time)
	redis.call("EXPIRE",key,3600)
	return {0,0} -- denying req
end
-- allowing req
tokens= tokens -1
redis.call("HMSET",key,"tokens",tokens,"last_refill",curr_time)
redis.call("EXPIRE",key,3600)
return {1,math.floor(tokens)}
`

func RateLimit(rdb *redis.Client, maxTokens int, refillRate float64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// get user id stored by auth in context (safe extraction)
			v := r.Context().Value("user_id")
			if v == nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			userID, ok := v.(string)
			if !ok || userID == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			key := "ratelimit:user:" + userID
			curr_time := float64(time.Now().Local().UnixMilli()) / 1000.0

			result , err := redis.NewScript(tokenBucketLuaScript).Run(
				context.Background(),
				rdb,
				[]string{key},
				curr_time,maxTokens,refillRate,
			).Slice()
			if err != nil{
				next.ServeHTTP(w,r)
				return
			}
			allowed := result[0].(int64)
			remaining := result[1].(int64)
			
			// Step 7 — always tell the client their limit status
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(maxTokens))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

			// Step 8 — rejected, bucket empty
			if allowed == 0 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			// Step 9 — allowed, continue to handler
			next.ServeHTTP(w, r)

		})
	}

}
