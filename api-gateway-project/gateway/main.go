package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RameshwariS/gateway/middleware"
	"github.com/redis/go-redis/v9"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// --- configuration -----------------------------------------------------
	// JWT_SECRET has no fallback: a missing secret now stops the process
	// instead of silently signing/verifying with a well-known default.
	secret := mustEnv("JWT_SECRET")
	if len(secret) < 32 {
		slog.Error("JWT_SECRET is too short", "min_length", 32)
		os.Exit(1)
	}

	userServiceURL := serviceURL("USER_SERVICE")
	itemServiceURL := serviceURL("ITEM_SERVICE")

	// --- Redis ---------------------------------------------------------------
	// Two supported shapes: REDIS_URL, a full connection string (this is
	// what Render's managed Key Value gives you, and what Redis Cloud/Upstash
	// style providers typically use too), or REDIS_ADDR, a bare host:port
	// (what Docker Compose's local redis service uses in .env.example).
	// REDIS_URL wins if both are set.
	var redisOpts *redis.Options
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		var err error
		redisOpts, err = redis.ParseURL(redisURL)
		if err != nil {
			slog.Error("invalid REDIS_URL", "err", err)
			os.Exit(1)
		}
	} else {
		redisOpts = &redis.Options{Addr: mustEnv("REDIS_ADDR")}
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelPing()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		slog.Warn("redis not reachable at startup", "addr", redisOpts.Addr, "err", err)
	} else {
		slog.Info("redis connected", "addr", redisOpts.Addr)
	}

	// --- routing ---------------------------------------------------------------
	userURL, itemURL := mustURL(userServiceURL), mustURL(itemServiceURL)
	routes := []Route{
		{Prefix: "/users", Proxy: newProxy(userURL)},
		{Prefix: "/items", Proxy: newProxy(itemURL)},
	}
	warmUp(userURL, itemURL)
	router := newRouter(routes)

	rateLimitCfg := middleware.RateLimitConfig{
		Max:      10,
		Rate:     2.0,
		Timeout:  50 * time.Millisecond,
		FailOpen: true, // preserve availability when Redis is temporarily unreachable.
	}

	// Deliberately tighter than the per-user limit above: /auth/login is
	// unauthenticated and mints a JWT on every successful call, so with no
	// limit at all it would be the one route that lets someone route around
	// the per-user rate limiting entirely (call it in a loop, get a fresh
	// user id worth of quota each time). Keyed by IP, not by user, since
	// nobody has a verified identity yet at this point in the chain.
	loginRateLimitCfg := middleware.RateLimitConfig{
		Max:      5,
		Rate:     0.1, // 1 token per 10s after the initial burst of 5
		Timeout:  50 * time.Millisecond,
		FailOpen: true,
	}

	// --- middleware chain --------------------------------------------------
	// Order matters:
	//   RequestID -> Logger -> Recover -> Auth -> RateLimit -> router
	// RequestID and Logger run for every request, including ones later
	// rejected, so every outcome (200, 401, 429, panic) is still logged.
	// Recover sits inside Logger so a panic is still timed and logged as a
	// clean 500. Auth must run before RateLimit, which needs the verified
	// user ID Auth places in the request context.
	protected := middleware.RequestID(
		middleware.Logger(
			middleware.Recover(
				middleware.Auth(secret)(
					middleware.RateLimitWithConfig(rdb, rateLimitCfg)(router),
				),
			),
		),
	)

	mux := http.NewServeMux()

	// /health is registered outside the protected chain on purpose: a
	// liveness check must never require a valid JWT, or the gateway could
	// lock its own monitoring out.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// /auth/login and /auth/demo-users are also unauthenticated, for the
	// same reason a real login page can't require you to already be logged
	// in. See authhandler.go for what this endpoint does and does not do.
	// login is additionally rate limited by IP (see loginRateLimitCfg above)
	// -- it is the one unauthenticated route that does real work (signing a
	// token), so it cannot be left completely open the way a static
	// demo-users list can.
	auth := &authHandlers{secret: secret}
	mux.Handle("/auth/login", middleware.RequestID(middleware.Logger(middleware.Recover(
		middleware.RateLimitByIP(rdb, loginRateLimitCfg)(http.HandlerFunc(auth.login)),
	))))
	mux.HandleFunc("/auth/demo-users", auth.listDemoUsers)

	// /ready additionally checks the gateway's dependency (Redis), for use
	// as a Kubernetes readiness probe: a gateway that can't reach Redis
	// should stop receiving traffic even if the process itself is alive.
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
		defer cancel()
		if err := rdb.Ping(ctx).Err(); err != nil {
			middleware.WriteError(w, http.StatusServiceUnavailable, "not_ready", "redis unavailable")
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.Handle("/", protected)

	// --- server --------------------------------------------------------------
	// PORT is set by Render (and by most other PaaS providers) to tell a web
	// service which port to bind; it defaults to 3000 for local Docker
	// Compose, where the port is fixed by docker-compose.yml instead.
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           middleware.CORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      120 * time.Second, // long enough to ride out a cold-starting free-tier backend
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("gateway starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// --- graceful shutdown ---------------------------------------------------
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
}
