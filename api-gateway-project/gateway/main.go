package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/RameshwariS/gateway/middleware"
	"github.com/redis/go-redis/v9"
)

type Route struct {
  Prefix string
  Proxy  http.Handler
}


func health_handler(res http.ResponseWriter, req *http.Request) {
	res.WriteHeader(http.StatusOK)
}

func handler(res http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	if strings.HasPrefix(path, "/users") {
		//proxy to user service
		target, _ := url.Parse("http://user-service:3002") // converts string to url object

		proxy := httputil.NewSingleHostReverseProxy(target) 

		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			fmt.Println("Proxy error:", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, `{"error":"service unavailable"}`)
		}

		proxy.ServeHTTP(res, req)
		return

	} else if strings.HasPrefix(path, "/products") {
		//proxy to product service
		target, _ := url.Parse("http://product-service:3001") //breaks the url in structure
		proxy := httputil.NewSingleHostReverseProxy(target)

		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			fmt.Println("Proxy error:", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, `{"error":"service unavailable"}`)
		}

		proxy.ServeHTTP(res, req)
		return
	} else if path == "/health" {
		res.WriteHeader(http.StatusOK)
	} else {
		res.WriteHeader(404)
		fmt.Fprintf(res, "Not Found")
	}

}

// The issue: if you wrap handler with middleware, it applies to all routes including /health. But you don't want /health to require a JWT token — health checks should always work.
// Fix: use a custom mux so you can control which routes get middleware and which don't.

func main() {
	// http.HandleFunc("/health",health_handler)

	// will take the input from the request and then check where to send
	// http.HandleFunc("/",handler)

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "my_key" // fallback for local dev
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	// passing the context
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	fmt.Println("context",ctx)

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Println("WARNING: Redis not reachable:", err)
	} else {
		fmt.Println("Redis connected:", redisAddr)
	}
	mux := http.NewServeMux()
	//no middleware
	mux.HandleFunc("/health", health_handler)

protected := middleware.RequestID(middleware.Logger(middleware.Recover(
  middleware.Auth(secret)(middleware.RateLimit(rdb, cfg)(router)))))

	mux.Handle("/", protected)

	// http.ListenAndServe(":3000", mux)
	// for graceful shutdown
	srv := &http.Server{
		Addr:    ":3000",
		Handler: mux,
		 ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,

	}

	go func() {
		fmt.Println("Gateway running on 3000")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Println("Server error", err)
		}
	}()

	// getting signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(),10*time.Second)
	defer shutdownCancel()
	
	if err := srv.Shutdown(shutdownCtx); err != nil{
		fmt.Println("forced shutdown",err)
	}
	fmt.Println("Gateway stopped")
}
