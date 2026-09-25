# GoProxyX — API Gateway

> Go · Redis · Docker · JWT

A production-style API Gateway built in Go. Acts as the single entry point for all client requests. Instead of clients calling `user-service` or `item-service` directly, every request passes through the gateway first.

---

## What It Does

Every request passes through four layers before reaching any service:

| Layer | What it does |
|---|---|
| **Logger** | Records every request — method, path, status code, latency |
| **Auth** | Validates the JWT token, rejects anything invalid with 401 |
| **RateLimit** | Enforces per-user request limits via Redis token bucket |
| **Handler** | Reverse proxies to the correct backend service |

---

## Architecture

```
Request arrives at :3000
   │
   ▼
Logger      ← starts timer, wraps ResponseWriter
   │
   ▼
Auth        ← checks JWT token
   │  ✗ invalid → 401 Unauthorized (stops here)
   │  ✓ valid   → stores user_id in context
   ▼
RateLimit   ← checks token bucket in Redis
   │  ✗ empty  → 429 Too Many Requests (stops here)
   │  ✓ tokens → deducts one token, continues
   ▼
Handler     ← picks service based on URL path
   ├── /users/*     → http://user-service:3002
   └── /items/*  → http://item-service:3001
   │
   ▼
Response travels back through Logger → logged with status + latency
```

---

## Project Structure

```
api-gateway-project/
├── gateway/
│   ├── main.go                    entry point — routing, middleware chain, server
│   ├── middleware/
│   │   ├── logger.go              logs every request
│   │   ├── auth.go                validates JWT tokens
│   │   └── ratelimit.go           token bucket rate limiting via Redis
│   ├── cmd/
│   │   └── gentoken/
│   │       └── main.go            CLI tool to generate test JWT tokens
│   ├── go.mod
│   └── Dockerfile
├── user-service/
│   ├── main.go                    GET /users  and  GET /users/{id}
│   └── Dockerfile
├── item-service/
│   ├── main.go                    GET /items  and  GET /items/{id}
│   └── Dockerfile
├── docker-compose.yml             runs all 4 containers together
└── config/
    └── .env                       environment variable reference
```

---

## Prerequisites

| Tool | Version | Check |
|---|---|---|
| Go | 1.20+ | `go version` |
| Docker | Any recent | `docker --version` |
| Docker Compose | Any recent | `docker-compose --version` |

---

## How to Run

**1. Clone the project**

```bash
git clone https://github.com/RameshwariS/api-gateway-project.git
cd api-gateway-project
```

**2. Start all containers**

```bash
docker-compose up --build
```

You should see:

```
gateway          | Redis connected: redis:6379
gateway          | Gateway running on :3000
user-service     | listening on :3002
item-service  | listening on :3001
```

> The gateway runs at `http://localhost:3000`
> All requests must go through the gateway — never call services directly.

---

## Generate a Test Token

Every request (except `/health`) requires a JWT token.

```bash
cd gateway
go run cmd/gentoken/main.go --user=u1 --secret=my_key
```

Copy the printed token. Use it in your requests.

| Flag | Default | Description |
|---|---|---|
| `--user` | *(required)* | User ID to embed in the token |
| `--secret` | `my_key` | Must match `JWT_SECRET` in docker-compose.yml |
| `--expires` | `24h` | How long the token is valid |

Generate tokens for different users:

```bash
go run cmd/gentoken/main.go --user=alice --secret=my_key
go run cmd/gentoken/main.go --user=bob   --secret=my_key
```

---

## API Endpoints

### Health Check
No token needed.
```bash
curl http://localhost:3000/health
# → 200 OK
```

---

### Get All Users
```bash
TOKEN="paste_your_token_here"

curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/users
```
Response:
```json
[
  { "id": 1, "name": "Alice" },
  { "id": 2, "name": "Bob" }
]
```

---

### Get a Specific User
```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/users/1
```
Response:
```json
{ "id": 1, "name": "Alice" }
```

---

### Get All Products
```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/items
```
Response:
```json
[
  { "id": 1, "name": "Phone" },
  { "id": 2, "name": "Laptop" }
]
```

---

### Get a Specific Product
```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/items/2
```
Response:
```json
{ "id": 2, "name": "Laptop" }
```

---

## Testing Every Feature

**Test 1 — No token (expect 401)**
```bash
curl -i http://localhost:3000/users
# → 401 Unauthorized
```

**Test 2 — Invalid token (expect 401)**
```bash
curl -i -H "Authorization: Bearer faketoken" http://localhost:3000/users
# → 401 Unauthorized
```

**Test 3 — Valid token (expect 200)**
```bash
TOKEN=$(go run cmd/gentoken/main.go --user=u1 --secret=my_key)
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:3000/users
# → 200 OK
```

**Test 4 — Rate limit (expect 429 after 10 requests)**
```bash
for i in $(seq 1 15); do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TOKEN" \
    http://localhost:3000/users)
  echo "Request $i → $STATUS"
done
```
Expected:
```
Request 1  → 200
Request 2  → 200
...
Request 10 → 200
Request 11 → 429
Request 12 → 429
...
Request 15 → 429
```

**Test 5 — Check rate limit headers**
```bash
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:3000/users
```
Look for:
```
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 9
```

**Test 6 — Inspect Redis directly**
```bash
docker-compose exec redis redis-cli
HGETALL ratelimit:user:u1
```
You will see:
```
tokens      → 0
last_refill → 1720000000.523
```

**Test 7 — Wait and retry**
```bash
sleep 5    # bucket refills at 2 tokens/sec — full again after 5 seconds
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:3000/users
# → 200 OK again
```

---

## Environment Variables

Set these variables in `api-gateway-project/.env` (copy `.env.example` as a starting point). Docker Compose passes them into the gateway container:

| Variable | Requirement | Description |
|---|---|---|
| `JWT_SECRET` | Required | Secret key for signing and verifying JWT tokens; use at least 32 characters |
| `REDIS_ADDR` | Required | Redis address, such as `redis:6379` inside Docker |
| `USER_SERVICE_URL` | Required | User service URL, such as `http://user-service:3002` inside Docker |
| `ITEM_SERVICE_URL` | Required | Product service URL, such as `http://item-service:3001` inside Docker |

> **Never commit real secrets to git.**
> Generate a secret with `openssl rand -hex 32` and use the same value when generating JWT tokens.

---

## Rate Limiting

Uses the **Token Bucket** algorithm. Each user gets their own bucket in Redis.

| Setting | Value | Meaning |
|---|---|---|
| Max tokens | 10 | Bucket capacity — max requests in a burst |
| Refill rate | 2.0 / second | Tokens added per second |
| Redis key | `ratelimit:user:{id}` | One bucket per user |
| Key TTL | 3600 seconds | Inactive buckets auto-delete after 1 hour |

**Response headers on every allowed request:**
```
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 7
```

**Response headers on 429:**
```
Retry-After: 1
```

**Reset a user's bucket for testing:**
```bash
docker-compose exec redis redis-cli
DEL ratelimit:user:u1
```

---

## Common Errors

| Status | Cause | Fix |
|---|---|---|
| `401` | Missing or invalid JWT token | Generate a token with the gentoken tool |
| `429` | Rate limit exceeded | Wait for bucket to refill — check `Retry-After` header |
| `404` | URL path has no matching route | Only `/users`, `/items`, `/health` are valid |
| `502` | Backend service is down | Check if user-service or item-service container is running |

---

## Useful Docker Commands

```bash
# Build and start all containers
docker-compose up --build

# Stop all containers
docker-compose down

# See gateway logs
docker-compose logs gateway

# Follow gateway logs live
docker-compose logs -f gateway

# Stop one service (to test 502 handling)
docker-compose stop user-service

# Open Redis CLI
docker-compose exec redis redis-cli
```

---

## Middleware Order Explained

```
Logger → Auth → RateLimit → Handler
```

| Position | Middleware | Why here |
|---|---|---|
| 1st (outermost) | Logger | Must wrap everything to log ALL outcomes including 401s and 429s |
| 2nd | Auth | Must run before RateLimit — RateLimit needs user_id that Auth puts in context |
| 3rd | RateLimit | Must run before Handler — no point forwarding a request you'll reject |
| 4th (innermost) | Handler | Only reached if Auth and RateLimit both passed |

**How Auth passes user_id to RateLimit:**

Go's request `context` is a key-value bag that travels with every request. Auth stores the user ID in it. RateLimit reads it out.

```go
// Auth stores:
context.WithValue(r.Context(), "user_id", "u1")

// RateLimit reads:
r.Context().Value("user_id").(string)
```

---

## How to Stop

```bash
# Stop all containers (data preserved)
docker-compose down

# Stop and delete all data including Redis contents
docker-compose down -v
```

The gateway shuts down gracefully — it finishes any in-progress requests before stopping.
