package main

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

// Route maps a path prefix to a backend proxy.
type Route struct {
	Prefix string
	Proxy  http.Handler
}

func mustEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		slog.Error("required environment variable is missing", "name", key)
		os.Exit(1)
	}
	return value
}

func mustURL(raw string) *url.URL {
	target, err := url.Parse(raw)
	if err != nil || target.Scheme == "" || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		slog.Error("invalid service URL", "url", raw)
		os.Exit(1)
	}
	return target
}

func newProxy(target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("upstream proxy error", "target", target.Host, "path", r.URL.Path, "err", err)
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"service unavailable"}`, http.StatusBadGateway)
	}
	return proxy
}

func newRouter(routes []Route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, route := range routes {
			if r.URL.Path == route.Prefix || strings.HasPrefix(r.URL.Path, route.Prefix+"/") {
				route.Proxy.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	})
}
