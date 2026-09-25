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

	redisAddr := mustEnv("REDIS_ADDR")
	userServiceURL := mustEnv("USER_SERVICE_URL")
	itemServiceURL := mustEnv("ITEM_SERVICE_URL")

	// --- Redis ---------------------------------------------------------------
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelPing()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		slog.Warn("redis not reachable at startup", "addr", redisAddr, "err", err)
	} else {
		slog.Info("redis connected", "addr", redisAddr)
	}

	// --- routing ---------------------------------------------------------------
	routes := []Route{
		{Prefix: "/users", Proxy: newProxy(mustURL(userServiceURL))},
		{Prefix: "/items", Proxy: newProxy(mustURL(itemServiceURL))},
	}
	router := newRouter(routes)

	rateLimitCfg := middleware.RateLimitConfig{
		Max:      10,
		Rate:     2.0,
		Timeout:  50 * time.Millisecond,
		FailOpen: true, // preserve availability when Redis is temporarily unreachable.
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
	srv := &http.Server{
		Addr:              ":3000",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
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
