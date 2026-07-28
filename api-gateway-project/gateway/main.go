package main

import (
	"fmt"
	"github.com/RameshwariS/gateway/middleware"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"github.com/redis/go-redis/v9"
	"context"
	"time"
)

func health_handler(res http.ResponseWriter, req *http.Request) {
	res.WriteHeader(http.StatusOK)
}

func handler(res http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	if strings.HasPrefix(path, "/users") {
		//proxy to user service
		target, _ := url.Parse("http://user-service:3002")  // converts string to url object
		proxy := httputil.NewSingleHostReverseProxy(target) // creates reverse proxy object
		proxy.ServeHTTP(res, req)
		return

	} else if strings.HasPrefix(path, "/products") {
		//proxy to product service
		target, _ := url.Parse("http://product-service:3001")
		proxy := httputil.NewSingleHostReverseProxy(target)
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
	if redisAddr ==""{
		redisAddr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctx,cancel := context.WithTimeout(context.Background(),3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
	   fmt.Println("WARNING: Redis not reachable:", err)
	} else {
		fmt.Println("Redis connected:", redisAddr)
	}
	mux := http.NewServeMux()
	//no middleware
	mux.HandleFunc("/health", health_handler)

	protected := middleware.Logger(
		middleware.Auth(secret)(
			middleware.RateLimit(rdb,10,2.0)(
				http.HandlerFunc(handler),
			),
		),
	)
	mux.Handle("/", protected)

	http.ListenAndServe(":3000", mux)
}
