# GoProxyX API Gateway: Code Explanation

This document explains every file reviewed in the project. It has two services:

- **gateway** (port 3000): the entry point. It authenticates, rate limits, logs and proxies requests.
- **item-service** (port 3001): a backend that stores items in Postgres.

```
client ──► gateway :3000 ──► /users → user service
                         └─► /items → item-service :3001 ──► Postgres
                 │
                 └─► Redis (rate limiting)
```

Request flow inside the gateway:

```
RequestID → Logger → Recover → Auth → RateLimit → router → reverse proxy
```

Contents:

1. [gateway/cmd/gentoken/main.go](#1-gatewaycmdgentokenmaingo)
2. [gateway/main.go](#2-gatewaymaingo)
3. [What a mux is](#3-what-a-mux-is)
4. [Server start and graceful shutdown](#4-server-start-and-graceful-shutdown)
5. [gateway/router.go](#5-gatewayroutergo)
6. [gateway/middleware/request.go](#6-gatewaymiddlewarerequestgo)
7. [gateway/middleware/ratelimit.go](#7-gatewaymiddlewareratelimitgo)
8. [gateway/middleware/logger.go](#8-gatewaymiddlewareloggergo)
9. [gateway/middleware/auth.go](#9-gatewaymiddlewareauthgo)
10. [item-service/handlers.go](#10-item-servicehandlersgo)
11. [item-service/main.go](#11-item-servicemaingo)
12. [item-service/store.go and types.go](#12-item-servicestorego-and-typesgo)
13. [Issues found, in one place](#13-issues-found-in-one-place)

---

## 1. gateway/cmd/gentoken/main.go

A small command-line tool that generates a signed JWT (JSON Web Token). You use it to get a valid token for testing the gateway's authentication.

**Flags**
- `--user` is required. It's the user ID stored in the token.
- `--secret` is the signing key. It defaults to `my_key`.
- `--expires` is the token lifetime. It defaults to 24 hours, and accepts values like `1h` or `30m`.

**Steps**
1. If `--user` is empty, the program exits with an error.
2. It builds the claims (the token payload):
   - `user_id` is the user you passed in.
   - `exp` is the expiry time, as a Unix timestamp.
   - `iat` is the issued-at time.
3. It creates the token with HS256 (HMAC-SHA256) and signs it with the secret. The result is a `header.payload.signature` string.
4. It prints the token to stdout.

```bash
go run ./cmd/gentoken --user alice --expires 1h
curl -H "Authorization: Bearer $(go run ./cmd/gentoken --user alice)" localhost:3000/items
```

**Notes**
- The secret must match the gateway's `JWT_SECRET`, or requests will be rejected. The gateway also requires the secret to be at least 32 characters, so pass `--secret` explicitly.
- The `my_key` default is for local development only.
- The item service parses the user ID as a number (see section 10), so use numeric users such as `--user 1`.

---

## 2. gateway/main.go

The entry point of the gateway. It reads its configuration, connects to Redis, sets up routing and middleware, starts the server, and shuts down cleanly.

### Imports
Standard library: `context` (timeouts), `log/slog` (structured logging), `net/http` (server), `os` (env vars, exit), `os/signal` and `syscall` (stop signals), `time`. Project and third party: the `middleware` package, and the go-redis client.

### Logging
```go
slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
```
Every `slog.Info`, `slog.Error` and so on now writes JSON to stdout.

### Configuration
- `JWT_SECRET` is required, with no fallback. If it's shorter than 32 characters, the gateway logs an error and exits. HS256 needs a long secret to be secure.
- `REDIS_ADDR`, `USER_SERVICE_URL` and `ITEM_SERVICE_URL` are also required. `mustEnv` exits if one is missing.

### Redis
- `redis.NewClient` creates the client. It doesn't connect yet, because go-redis connects lazily.
- `defer rdb.Close()` closes it when `main` returns.
- A context with a 3-second timeout is created, and Redis is pinged. If the ping fails, it only logs a warning and the gateway keeps running. This matches `FailOpen: true` below.

### Routing
```go
routes := []Route{
    {Prefix: "/users", Proxy: newProxy(mustURL(userServiceURL))},
    {Prefix: "/items", Proxy: newProxy(mustURL(itemServiceURL))},
}
router := newRouter(routes)
```
`mustURL` parses and validates each URL. `newProxy` builds a reverse proxy to it. `newRouter` returns a handler that picks the proxy whose prefix matches the path (see section 5).

### Rate limit config
```go
Max: 10, Rate: 2.0, Timeout: 50ms, FailOpen: true
```
A token bucket: a burst of 10 requests, refilling 2 tokens per second. Redis calls are given 50ms. `FailOpen` lets requests through if Redis is unreachable.

### Middleware chain
```go
protected := RequestID(Logger(Recover(Auth(secret)(RateLimitWithConfig(rdb, cfg)(router)))))
```
Each middleware wraps the next. The outermost runs first. The order matters:
- `RequestID` and `Logger` are outermost so every request is logged, including rejected ones (401, 429) and panics.
- `Recover` is inside `Logger`, so a panic is still logged as a clean 500 with timing.
- `Auth` must come before `RateLimit`, because the rate limiter needs the verified user ID that Auth stores in the context.

### Mux and health endpoints
- `/health` returns 200 and is registered **outside** the protected chain on purpose. A liveness check must not need a JWT, or the gateway could lock its own monitoring out.
- `/ready` pings Redis with a 500ms timeout. It returns 503 if Redis is down, so a Kubernetes readiness probe stops sending traffic to an instance that can't reach Redis.
- `mux.Handle("/", protected)` sends everything else through the protected chain.

### Server settings
- `Addr: ":3000"`
- `ReadHeaderTimeout: 5s` guards against slowloris attacks (clients that send headers very slowly).
- `ReadTimeout: 10s`, `WriteTimeout: 30s` and `IdleTimeout: 60s` bound the rest of the connection lifecycle.

---

## 3. What a mux is

A **mux** (multiplexer) is a request router. It looks at each request's URL path and picks the handler to run. `http.NewServeMux()` is Go's built-in one.

```go
mux.HandleFunc("/health", ...)   // exact path → health handler
mux.HandleFunc("/ready", ...)    // exact path → readiness handler
mux.Handle("/", protected)       // everything else → protected chain
```

| Request | Handler |
|---|---|
| `GET /health` | plain 200, no auth |
| `GET /ready` | Redis check, no auth |
| `GET /users/123` | `protected` chain |
| `GET /anything-else` | `protected` chain |

Rules:
- The most specific pattern wins, so `/health` beats `/`.
- A pattern ending in `/` matches everything below it, so `"/"` is a catch-all.
- A pattern without a trailing slash matches only that exact path.

The mux is itself an `http.Handler`, which is why it can be passed to the server as `Handler: mux`.

Don't confuse it with the custom `router` (section 5). The mux decides between the public health endpoints and the protected chain. The router decides which backend service to proxy to.

---

## 4. Server start and graceful shutdown

```go
go func() {
    slog.Info("gateway starting", "addr", srv.Addr)
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        slog.Error("server error", "err", err)
        os.Exit(1)
    }
}()

quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit

slog.Info("shutting down")
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
defer shutdownCancel()
if err := srv.Shutdown(shutdownCtx); err != nil {
    slog.Error("forced shutdown", "err", err)
}
slog.Info("gateway stopped")
```

**Starting.** `go func() {...}()` runs the server in a separate goroutine, because `ListenAndServe` blocks forever and `main` needs to carry on to wait for signals. `ListenAndServe` always returns an error. `http.ErrServerClosed` is the normal result of a graceful shutdown and is ignored. Any other error, such as the port being in use, is logged and exits with status 1.

**Waiting for a signal.**
- `make(chan os.Signal, 1)` creates a channel with a buffer of 1. The buffer is required, because `signal.Notify` doesn't block when sending.
- `signal.Notify` delivers SIGINT (Ctrl+C) and SIGTERM (sent by `docker stop` and Kubernetes) to the channel instead of killing the process immediately. SIGKILL can't be caught.
- `<-quit` blocks `main` until a signal arrives.

**Shutting down.** A 10-second deadline is created. `srv.Shutdown` then:
1. closes the listener, so no new connections are accepted,
2. closes idle connections,
3. waits for in-flight requests to finish.

If requests are still running after 10 seconds, it returns an error and the code logs "forced shutdown". When `main` returns, the deferred calls run (`rdb.Close()` and the cancel functions).

```
start:   main ─ spawns goroutine ─► ListenAndServe (serving requests)
            │
            └─ blocks on <-quit
SIGTERM: <-quit unblocks
            └─ Shutdown() → ListenAndServe returns ErrServerClosed (ignored)
            └─ in-flight requests drain (max 10s)
            └─ log "gateway stopped" → main returns → process exits
```

Without graceful shutdown, a deploy or restart would cut off requests that are in progress.

---

## 5. gateway/router.go

### `Route` and the helpers
```go
type Route struct {
    Prefix string
    Proxy  http.Handler
}
```
A route pairs a path prefix with a handler. `Proxy` is the `http.Handler` interface, so any handler (including a test mock) can be a target.

- `mustEnv(key)` reads an env var and trims whitespace. If it's empty, it logs and exits. A value of just spaces counts as missing.
- `mustURL(raw)` parses a URL and exits if it's invalid. It requires a scheme of `http` or `https` and a non-empty host. This catches typos like `user-service:8080`, which `url.Parse` would accept with an odd result.

### `newProxy` (lines 36–44)
```go
proxy := httputil.NewSingleHostReverseProxy(target)
```
This is the standard library's reverse proxy. It rewrites the request to point at `target`, forwards it, and streams the response back. It also adds `X-Forwarded-For`.

```go
proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
    slog.Error("upstream proxy error", "target", target.Host, "path", r.URL.Path, "err", err)
    w.Header().Set("Content-Type", "application/json")
    http.Error(w, `{"error":"service unavailable"}`, http.StatusBadGateway)
}
```
This runs when the backend can't be reached (connection refused, DNS failure, timeout). The default would log plain text and return an empty 502. This version logs structured JSON and returns a JSON 502 body.

### `newRouter` (lines 46–56)
```go
for _, route := range routes {
    if r.URL.Path == route.Prefix || strings.HasPrefix(r.URL.Path, route.Prefix+"/") {
        route.Proxy.ServeHTTP(w, r)
        return
    }
}
http.NotFound(w, r)
```
It loops over the routes in order. The first match wins, and the request is handed to that proxy. If nothing matches, it returns 404.

### Design decisions
1. **Segment-aware prefix matching.** A plain `HasPrefix(path, "/users")` would also match `/usersettings` and send it to the user service. Requiring an exact match or a prefix followed by `/` avoids that.
2. **A hand-written router** returns a plain `http.Handler`, so it slots into the middleware chain as the innermost handler.
3. **Fail fast on config.** A bad config stops the process at startup instead of failing at the first request.
4. **Routes are passed in as a slice.** There's no global state. Adding a service is one line in `main.go`, and tests can pass fake routes.
5. **502 for upstream failures.** It tells clients the fault isn't in their request, and monitoring can tell it apart from the gateway's own 500s. The body is generic so internal hostnames don't leak. They go to the logs only.
6. **A linear scan** is fine for a few routes.

### Things to be aware of
- The proxy forwards the full original path. `/users/123` reaches the backend as `/users/123`. The prefix is not stripped.
- `Host` stays as the client's original value. Backends behind virtual hosting may need it rewritten.
- The proxy uses `http.DefaultTransport`, which has no overall response timeout. A hung backend is bounded only by the server's `WriteTimeout` (30s).
- There is also a `reverse_proxy.go` in the same directory. It wasn't reviewed, so it's unknown whether it duplicates `newProxy`.

---

## 6. gateway/middleware/request.go

Two middlewares: `RequestID` and `Recover`.

### `RequestID`
Think of a reception desk that gives every visitor a numbered badge. If something goes wrong later, you can refer to the visitor by their number.

```go
var raw [16]byte
rand.Read(raw[:])                  // 1. 16 random bytes (crypto/rand: secure, unpredictable)
id := hex.EncodeToString(raw[:])   // 2. text like "9f86d081884c7d65..." (32 characters)
w.Header().Set("X-Request-ID", id) // 3. tell the client its ID
ctx := context.WithValue(r.Context(), requestIDKey{}, id) // 4. attach it to the request
next.ServeHTTP(w, r.WithContext(ctx))                     // 5. pass it on
```
- If random generation fails, it logs and returns 500 without calling the next handler.
- `requestIDKey` is an empty private struct type used as the context key, so no other package can collide with it.
- `RequestIDFrom(ctx)` reads the ID back. It type-asserts to `string` with `_`, so it returns `""` instead of panicking when the key is missing. The logger and `Recover` use it.

### `Recover`
A **panic** in Go is a serious runtime error, such as using a nil pointer. `Recover` catches it so one bad request doesn't take the gateway down.

```go
wrapped := &recoverWriter{ResponseWriter: w}
defer func() {
    if recovered := recover(); recovered != nil {
        slog.Error("http handler panic", "request_id", ..., "panic", recovered)
        if !wrapped.committed {
            WriteError(w, 500, "internal_error", "internal server error")
        }
    }
}()
next.ServeHTTP(wrapped, r)
```
- `defer` runs the function when the handler ends, even if it ends in a panic.
- `recover()` stops the panic and returns what went wrong, or `nil` if nothing did.

**Why `recoverWriter` exists.** A response sends the status line first, then the body. Once that's sent, you can't change your mind. If a handler has already started responding and then panics, a 500 can no longer be sent. `recoverWriter` wraps the real writer and tracks one fact, `committed`: has anything been sent yet? `WriteHeader` and `Write` set it to true. After a panic, a 500 is sent only if `!committed`. `Unwrap` exposes the real writer so features like flushing still work.

### Design decisions
- `RequestID` runs first so every later log line can include the ID, and `Recover` runs inside `Logger`.
- IDs are random and carry no information.
- Fail closed: if the ID can't be generated, the request is rejected.
- Request-scoped data travels in `context`, which is safe for concurrent requests.

### Things to be aware of
- `RequestID` ignores any `X-Request-ID` the client sent, which is safe against log injection but breaks end-to-end tracing across services. The ID also isn't forwarded to the backends.
- `recoverWriter` doesn't implement `Flush` or `Hijack` directly. They're reachable only through `Unwrap` and `http.ResponseController`.
- `recover()` also catches `http.ErrAbortHandler`, which the standard library uses to abort a response deliberately.

---

## 7. gateway/middleware/ratelimit.go

Limits how many requests each user can make, using a **token bucket** stored in Redis.

### The idea
Each user has a bucket of tokens. It holds at most 10 (`Max`). Each request costs 1. Tokens refill at 2 per second (`Rate`). An empty bucket means a 429 response. A user can burst 10 requests, then is held to about 2 per second.

### The Lua script
The logic runs **inside Redis** as a Lua script. Without that, the gateway would read the bucket, calculate, and write it back, and two simultaneous requests could both see "1 token left" and both be allowed. A Lua script runs **atomically**, so nothing can interleave.

Inputs: `KEYS[1]` is the Redis key (e.g. `ratelimit:user:u1`). `ARGV[1]` is max tokens, `ARGV[2]` is the refill rate, `ARGV[3]` is the cost.

| Step | What it does |
|---|---|
| Read the clock | `redis.call('TIME')` uses **Redis's** clock, so multiple gateway instances agree even if their system clocks differ. |
| Load state | `HMGET` reads `tokens` and `last_refill` for the user. |
| First-time user | Starts with a full bucket. |
| Refill | `tokens + elapsed * rate`, capped at `max_tokens`. `math.max(0, ...)` guards against the clock going backwards. |
| Consume | If `tokens >= cost`, subtract the cost and allow. |
| Reject | Otherwise compute `retry_after = ceil((cost - tokens) / rate)`. |
| Save | `HSET` stores the new state, and `EXPIRE` deletes the key after twice the full-refill time so idle users don't pile up. |
| Return | `{allowed, floor(tokens), retry_after}` |

`redis.NewScript` wraps the script. go-redis sends it by hash (`EVALSHA`) and falls back to the full script if Redis doesn't have it cached.

### Config
```go
type RateLimitConfig struct {
    Max      int           // bucket size
    Rate     float64       // tokens refilled per second
    Timeout  time.Duration // how long to wait for Redis
    FailOpen bool          // what to do if Redis is broken
}
```
`RateLimit(...)` is the older, simpler constructor and uses `FailOpen: true`. `main.go` uses `RateLimitWithConfig`.

### The middleware, per request
1. **Identify the user** with `UserIDFrom(ctx)`. If missing, 401. This is why Auth must run first.
2. **Validate the config.** A nil Redis client or a `Max` or `Rate` of zero or less returns 503. This also avoids dividing by zero in Lua.
3. **Set a timeout** (default 100ms) so a slow Redis can't hang requests.
4. **Run the script** with key `"ratelimit:user:" + uid`.
5. **Handle Redis failure.** Controlled by `FailOpen`:
   - true: log and let the request through. Availability is preserved, but limiting is off while Redis is down.
   - false: return 503. Limits are strict, but a Redis outage becomes a gateway outage.
6. **Set headers.** `X-RateLimit-Limit` and `X-RateLimit-Remaining` are always set. If rejected, `Retry-After` is added and the response is **429 Too Many Requests**.
7. **Allow:** call the next handler.

### Design decisions
- Redis-backed, so the limit is shared across all gateway instances.
- Lua for atomicity (no read-modify-write race).
- Redis clock (no skew between instances).
- Per-user keys, not per IP.
- Auto-expiring keys keep Redis memory bounded.
- A short timeout plus fail-open means the limiter can't take the gateway down.

### Things to be aware of
- The comment block at the top of the file (lines 14–18) is outdated. It says ARGV[1] is the current time, but the script takes max, rate and cost, and reads the time from Redis.
- With fail-open, a Redis outage silently disables rate limiting. Only the error logs show it.
- `Remaining` is rounded down, though the bucket holds fractional tokens internally.
- `/health` and `/ready` are outside this chain, so they are never limited.

---

## 8. gateway/middleware/logger.go

Logs one line for every request: method, path, status code, latency and request ID.

### The problem it solves
The status code is chosen by a handler later in the chain, and `http.ResponseWriter` has no way to read it back, since it's write-only. The fix is to **wrap** the writer in a struct that remembers the status as it passes through.

### The wrapper
```go
type wrappedWriter struct {
    http.ResponseWriter   // embedded: its methods become available on the wrapper
    status_code int
    wroteHeader bool
}
```
- `WriteHeader(code)` ignores repeat calls (Go honors only the first). Otherwise it saves the code and calls the real `WriteHeader`.
- `Write(body)` first calls `WriteHeader(200)` if nothing was set, because Go implicitly sends 200 in that case. Then it forwards the body.
- `Unwrap()` returns the real writer, so `http.ResponseController` and the reverse proxy can reach flushing and similar features.

### The middleware
1. `start := time.Now()`
2. Wrap the writer, with a default status of 200.
3. `next.ServeHTTP(wrapped, r)` runs the rest of the chain.
4. After it returns, log: `method`, `path`, `status`, `latency_ms` and `request_id`.

```json
{"level":"INFO","msg":"http request","method":"GET","path":"/users/1","status":200,"latency_ms":12,"request_id":"9f86d081..."}
```

### Design decisions
- The wrapper is the standard way to capture a status code in Go middleware.
- Logging after the handler returns is the only way to know the final status and total latency.
- Logger is outside Recover and Auth, so 401s, 429s and panic-turned-500s are all logged.
- Only `r.URL.Path` is logged, not the query string. That's good for privacy, because query strings can contain tokens.

### Things to be aware of
- `wrappedWriter` is nearly a duplicate of `recoverWriter` in `request.go`. They could be merged into one shared wrapper.
- The log line has no user ID, because Auth sets it in a context that the outer layer's `r` can't see.
- The field name `status_code` uses snake_case, which is unusual in Go (it doesn't affect behavior).

---

## 9. gateway/middleware/auth.go

Checks that each request carries a valid JWT. If it does, the user's ID is stored in the request context. If not, the request is rejected with a 401. The file also defines `WriteError`, the shared JSON error helper.

### Storing the user ID safely
```go
type ctxKey string
const userIDKey ctxKey = "user_id"
```
A custom key type can't collide with other packages' keys. Because `userIDKey` isn't exported, no outside code can put a fake user ID in the context.

`UserIDFrom(ctx)` is the only accessor. It returns `(id, true)` only when the value exists and is non-empty.

### Extracting the token
```go
_, tok, found := strings.Cut(r.Header.Get("Authorization"), " ")
```
It splits the header at the first space. For `Bearer eyJ...`, `tok` is the token. If there's no token, it returns false.

Note: the first word is **not checked**. `Basic <jwt>` would be accepted.

### The `Auth` middleware
`Auth(secret)` returns a function that takes the next handler and returns a handler. The secret is captured once at startup and converted to bytes once.

Per request:
1. **Get the token.** If missing, return 401 `missing_token`.
2. **Parse and verify:**
   ```go
   jwt.ParseWithClaims(raw, claims,
       func(*jwt.Token) (any, error) { return key, nil },
       jwt.WithValidMethods([]string{"HS256"}),
       jwt.WithExpirationRequired(),
   )
   ```
   - The key function tells the library which key to verify with.
   - `WithValidMethods(HS256)` accepts only HS256. This blocks the **algorithm confusion attack** (e.g. a token claiming `alg: none`).
   - `WithExpirationRequired()` rejects tokens with no `exp`, so none are valid forever.
   - The library also checks the signature and time claims.
3. **On any error**, return 401 `invalid_token` with a deliberately vague message, so a client can't tell expired from forged.
4. **Extract `user_id`.** It must be a non-empty string, or the response is 401.
5. **Store it** in the context and call the next handler.

### `WriteError`
```go
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
```
This gives the whole gateway one error shape:
```json
{"error":{"code":"invalid_token","message":"token invalid or expired"}}
```
The order matters: headers, then status, then body. Clients can branch on the stable `code` field.

### Design decisions
- **Stateless authentication.** No database lookup is needed, since the signature proves the token is genuine.
- **Algorithm pinning plus required expiry** defend against common JWT mistakes.
- **Auth at the edge**, so backends don't each verify tokens.
- **Generic error messages** reveal little to attackers.
- **An encapsulated context key** keeps the identity trustworthy.

### Things to be aware of
- The `Bearer` scheme isn't enforced (see above).
- **The user ID isn't forwarded to backends.** The proxy passes the original `Authorization` header through, but nothing sets an `X-User-ID` header (see section 13).
- There is no revocation. A valid token works until it expires, unless you add state such as a Redis denylist.
- One shared secret: anyone who has it (including `gentoken`) can mint tokens for any user.
- Only HS256 is supported, with no key rotation or asymmetric keys.

---

## 10. item-service/handlers.go

The HTTP handlers for the item backend. Each reads the request, calls the store, and writes JSON.

| Request | Handler |
|---|---|
| `GET /items` | `ListItems` |
| `POST /items` | `CreateItem` |
| `GET /items/{id}` | `GetItem` |
| `POST /items/{id}/purchase` | `Purchase` |

### Setup
- `Handlers` holds a pointer to the `Store`. The handlers never touch the database directly.
- `writeError` writes the same JSON error shape as the gateway.
- `callerID` reads the `X-User-ID` header and parses it as a base-10 64-bit integer. It returns `(id, true)`, or `(0, false)` if the header is missing or not a number.

### `ListItems`
- Reads `?q=` (search) and `?limit=`. The limit defaults to 20 and only 1–100 is accepted. Anything else silently falls back to 20.
- Calls the store with the request context, so a cancelled request cancels the query. The empty `sql.NullTime{}` and `sql.NullInt64{}` are pagination cursors meaning "start from the beginning".
- On error, returns a generic 500.
- Converts a `nil` slice to an empty one, so the JSON is `[]` instead of `null`.

### `GetItem`
```go
switch {
case errors.Is(err, ErrNotFound): 404
case err != nil:                  500
default:                          encode the item
}
```
`errors.Is` matches even wrapped errors. The specific case must come before the generic one.

### `CreateItem`
`createItemRequest` maps JSON fields (`title`, `description`, `price_jpy`). The price is an integer, because yen has no fractional units, and this avoids floating-point money bugs.

1. Get the seller from `callerID`, or 401.
2. Decode the JSON body, or 400.
3. Validate: the title must be non-blank and the price positive, or 400.
4. Call the store, or a generic 500.
5. Respond **201 Created** with the new item. `WriteHeader(201)` must come before the body.

### `Purchase`
1. Get the buyer from `callerID`, or 401.
2. Call `store.Purchase`.
3. Map each business error to a precise status:

| Error | Status | Meaning |
|---|---|---|
| `ErrNotFound` | 404 | no such item |
| `ErrAlreadySold` | 409 Conflict | someone else got it first |
| `ErrOwnItem` | 403 Forbidden | buyer is the seller |
| anything else | 500 | unexpected failure |
| none | 200 | returns the order |

### Design decisions
- Layered: handlers deal with HTTP and the store deals with SQL.
- Sentinel errors are mapped to precise statuses.
- Internal errors aren't leaked to clients.
- The request context is passed to every store call.
- List size is capped at 100.
- Identity comes from a header: the service trusts the gateway.

### Things to be aware of
- **`X-User-ID` is never set by the gateway** (only mentioned in this file), so `CreateItem` and `Purchase` would always return 401 through the gateway (section 13).
- The item service must be unreachable except through the gateway. Otherwise clients can set the header themselves and impersonate anyone.
- The JWT's `user_id` is a string but `callerID` needs a number, so tokens must use numeric IDs.
- Errors aren't logged. The 500 branches discard the real error.
- Success responses set no `Content-Type`, so Go sniffs it (probably as `text/plain`).
- There is no request body size limit. `http.MaxBytesReader` would add one.
- Pagination is half-built: cursors are always empty and no next cursor is returned.

---

## 11. item-service/main.go

The entry point of the item service. It connects to Postgres, builds the handlers, defines the routes and starts a server on port 3001.

### Setup
- `_ "github.com/lib/pq"` is a blank import. It registers the `"postgres"` driver as a side effect. `sql.Open("postgres", ...)` wouldn't work without it.
- `DATABASE_URL` is required, or the service exits.
- `sql.Open` doesn't connect. It creates the connection pool.
- **Pool tuning:**
  - `SetMaxOpenConns(10)` caps simultaneous connections, protecting Postgres.
  - `SetMaxIdleConns(5)` keeps 5 ready for reuse.
  - `SetConnMaxLifetime(30m)` recycles connections, avoiding stale ones broken by firewalls or database restarts.
- A 3-second `PingContext` actually connects. Unlike the gateway's Redis ping, a failure here **exits**, because the service is useless without its database.
- `NewStore(db)` and `&Handlers{store: store}` wire the layers together by hand (dependency injection).

### Routing (standard `ServeMux`)
- `/health` returns 200.
- `/items` is an exact match. GET lists, POST creates, anything else returns 405.
- `/items/` is a prefix match. The path is parsed by hand:
  1. `TrimPrefix` removes `/items/`.
  2. `Split(rest, "/")` gives `["42"]` or `["42", "purchase"]`.
  3. `parts[0]` must be a number, or 400 `invalid_id`.
  4. A `switch` picks the handler: `GET /items/42` → `GetItem`, `POST /items/42/purchase` → `Purchase`, anything else → 404.

### Server
It listens on `:3001`, with `ReadHeaderTimeout: 5s`. `ListenAndServe` runs on the main goroutine, and any returned error is logged before exiting.

### Design decisions
- Fail fast on missing config or an unreachable database.
- A bounded connection pool protects Postgres.
- The standard library alone is enough for four routes.

### Things to be aware of
- **No graceful shutdown.** On SIGTERM the process dies immediately and in-flight requests are cut off.
- Only `ReadHeaderTimeout` is set. There's no `ReadTimeout`, `WriteTimeout` or `IdleTimeout`, unlike the gateway.
- **The service has no authentication.** It trusts `X-User-ID` completely, so port 3001 must not be exposed.
- No panic recovery or request logging.
- `/items/42/` (trailing slash) falls to the 404 default, and a negative id like `/items/-5` parses and goes on to the store.
- 404 and 405 responses have empty bodies, while the handlers return JSON errors.
- `/health` doesn't check the database, so there's no readiness check.

---

## 12. item-service/store.go and types.go

The **data layer**: all the SQL lives here.

### types.go
- `Item` has a field per column. `BuyerID *int64` and `SoldAt *time.Time` are **pointers** because they're NULL until the item is sold. A nil pointer is Go's NULL, and `omitempty` leaves it out of the JSON.
- `Order` is the receipt created when an item is bought.
- The `json:"..."` tags set the API field names.

### Errors and the struct
```go
ErrNotFound, ErrAlreadySold, ErrOwnItem
```
These are **sentinel errors**. The store returns them and handlers use `errors.Is` to pick an HTTP status, so the store knows nothing about HTTP.

`Store` wraps the `*sql.DB` pool. Every method takes a `ctx`, and the `...Context` query functions cancel the query if the request is cancelled or times out.

### `ListItems`
```sql
SELECT ... FROM items
WHERE status = 'on_sale'
  AND ($1 = '' OR title ILIKE '%' || $1 || '%')
  AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
ORDER BY created_at DESC, id DESC
LIMIT $4
```
- Only unsold items are shown.
- **Search:** an empty `$1` matches everything. Otherwise `ILIKE` does a case-insensitive "contains".
- **Keyset pagination:** if the cursor is NULL, start at the top. Otherwise return rows strictly older than `(created_at, id)`. The `id` breaks ties between equal timestamps. This stays fast on deep pages and doesn't skip or repeat rows when new items arrive, unlike `OFFSET`.
- `$1`..`$4` are **placeholders**. Values are sent separately from the query text, so user input can't change the SQL. This prevents SQL injection.
- `QueryContext` returns `rows`, `defer rows.Close()` returns the connection to the pool, and `rows.Next()` and `Scan` copy each row into an `Item`. `rows.Err()` catches errors during iteration.
- No rows leaves `items` as `nil`, which the handler turns into `[]`.

### `GetItem`
`QueryRowContext` expects one row. `sql.ErrNoRows` is translated into the domain error `ErrNotFound`.

### `CreateItem`
`INSERT ... RETURNING ...` hands back the full row, including database-generated values (id, default status, `created_at`), in one round trip.

### `Purchase`
The most important function. Buying needs several steps that must succeed or fail together, so it uses a **transaction**.

1. `BeginTx`, plus `defer tx.Rollback()` as a safety net. After a successful commit, the rollback does nothing.
2. `SELECT status, seller_id, price_jpy FROM items WHERE id = $1 FOR UPDATE`. `FOR UPDATE` **locks the row** until the transaction ends. Without it, two buyers could both read "on_sale" and both succeed. With it, the second buyer waits, then sees `sold`.
3. Business rules on the locked row: no row → `ErrNotFound`, seller equals buyer → `ErrOwnItem`, status not `on_sale` → `ErrAlreadySold`.
4. `UPDATE` marks the item sold with the buyer and `sold_at = now()`.
5. `INSERT INTO orders ... RETURNING` creates the order. The **price is copied** from the item at purchase time, so the order keeps the price paid.
6. `tx.Commit()` makes it permanent. If steps 4–6 fail, the rollback undoes everything.

### Design decisions
- Separation of layers (SQL and domain errors here, HTTP in the handlers).
- Parameterized queries only.
- Pessimistic locking guarantees one buyer wins.
- Keyset pagination scales better than `OFFSET`.
- A transaction keeps the multi-step purchase consistent.
- The order stores a price snapshot.

### Things to be aware of
- Pagination isn't usable end to end yet (handler passes empty cursors and returns no next cursor).
- Search text isn't escaped, so a `%` or `_` in `q` acts as a wildcard (a correctness quirk, not injection).
- `ILIKE '%...%'` can't use a normal index. A trigram index (`pg_trgm`) would help on large tables.
- `ErrOwnItem` is checked before `ErrAlreadySold`, so someone viewing their own sold item gets "own item".
- The file mixes tabs and spaces. `gofmt` would normalize it.
- A unique index on `orders(item_id)` would add a second layer of protection against duplicate orders. The migrations weren't reviewed, so one may already exist.

---

## 13. Issues found, in one place

Ordered by importance.

1. **The gateway never sets `X-User-ID`, but the item service requires it.** Through the gateway, `POST /items` and `POST /items/{id}/purchase` would always return 401. Fix: in the proxy, set `X-User-ID` from `UserIDFrom(ctx)`, and strip any client-supplied `X-User-ID` first so it can't be spoofed.
2. **The item service trusts the header completely and has no auth of its own.** Make sure port 3001 is reachable only from the gateway.
3. **ID type mismatch.** `callerID` parses a number, but a JWT `user_id` is a string. Tokens must carry numeric IDs (not `alice`).
4. **Auth doesn't verify the `Bearer` scheme.** Any first word is accepted.
5. **item-service has no graceful shutdown**, and only one server timeout is set.
6. **Item service handlers discard errors without logging**, which makes failures hard to debug.
7. **Rate limiting fails open.** A Redis outage silently disables it (a documented trade-off, but add an alert).
8. **No token revocation**, and a single shared HS256 secret.
9. **Outdated comment** at the top of `ratelimit.go` (it lists ARGV inputs that no longer match the script).
10. **Smaller items:** missing `Content-Type` on success responses, no request body size limit, empty 404/405 bodies, no `Host` rewrite in the proxy, pagination half-built, duplicated response-writer wrappers, inconsistent formatting.

> `gateway/reverse_proxy.go` and the migrations were not reviewed, so the notes above that depend on them are marked as unverified.
