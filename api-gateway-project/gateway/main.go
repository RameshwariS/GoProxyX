package main

import (
	"fmt"
	"github.com/RameshwariS/gateway/middleware"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
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

	mux := http.NewServeMux()
	//no middleware
	mux.HandleFunc("/health", health_handler)

	protected := middleware.Logger(
		middleware.Auth(secret)(
			http.HandlerFunc(handler),
		),
	)
	mux.Handle("/", protected)

	http.ListenAndServe(":3000", mux)
}
