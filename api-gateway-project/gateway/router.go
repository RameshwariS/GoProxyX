package main

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/RameshwariS/gateway/middleware"
)

// Route maps a path prefix to a backend proxy.
type Route struct {
	Prefix string
	Proxy  http.Handler
}

// gatewaySecret is shared with the backend services via the GATEWAY_SECRET
// environment variable; empty means "don't send one" (local development).
var gatewaySecret = strings.TrimSpace(os.Getenv("GATEWAY_SECRET"))

// warmUp pings each backend's /health once, in the background, ignoring the
// result. On Render's free plan a web service that has been idle for ~15
// minutes is spun down and takes up to a minute to wake on its next request;
// hitting /health as soon as the gateway itself boots means the backends are
// waking up in parallel while the user is still on the login screen, rather
// than the first real request waiting for a cold start behind the gateway's.
func warmUp(targets ...*url.URL) {
	for _, t := range targets {
		go func(t *url.URL) {
			client := &http.Client{Timeout: 90 * time.Second}
			resp, err := client.Get(t.ResolveReference(&url.URL{Path: "/health"}).String())
			if err != nil {
				slog.Warn("warm-up ping failed", "target", t.Host, "err", err)
				return
			}
			resp.Body.Close()
			slog.Info("warm-up ping ok", "target", t.Host, "status", resp.StatusCode)
		}(t)
	}
}

// serviceURL resolves a backend's address from the environment, accepting
// two shapes so the same binary runs everywhere:
//
//	<PREFIX>_URL   a full URL or host:port (Docker Compose: http://item-service:3001)
//	<PREFIX>_HOST  a Render service slug; the public URL is https://<slug>.onrender.com
//
// On Render the Blueprint fills <PREFIX>_HOST from the backend service's own
// `host` property, which stays correct even when Render had to add a suffix
// to the service name because it was already taken.
func serviceURL(prefix string) string {
	if v := strings.TrimSpace(os.Getenv(prefix + "_URL")); v != "" {
		return v
	}
	if h := strings.TrimSpace(os.Getenv(prefix + "_HOST")); h != "" {
		return "https://" + h + ".onrender.com"
	}
	slog.Error("backend address is missing: set either " + prefix + "_URL or " + prefix + "_HOST")
	os.Exit(1)
	return ""
}

func mustEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		slog.Error("required environment variable is missing", "name", key)
		os.Exit(1)
	}
	return value
}

// mustURL parses raw as a backend URL. If raw has no scheme (as happens with
// Render's internal "host:port" addresses, e.g. from a Blueprint's
// fromService/hostport reference), http:// is assumed -- backend traffic on
// Render's private network, and on the Docker Compose network locally, does
// not need TLS.
func mustURL(raw string) *url.URL {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme == "" || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		slog.Error("invalid service URL", "url", raw)
		os.Exit(1)
	}
	return target
}

// newProxy builds a reverse proxy to target that forwards the caller's
// VERIFIED identity to the backend, not whatever the caller happened to
// send. Auth already parsed and validated the JWT and placed the resulting
// user id in the request context (middleware.UserIDFrom); this is the only
// value of X-User-ID a backend service should ever trust, so any
// client-supplied X-User-ID header is deleted first and replaced with the
// verified one. Backend services (see item-service/handlers.go) rely on
// this: they read X-User-ID directly and treat it as authoritative, which
// is only safe because they are unreachable except through this gateway.
func newProxy(target *url.URL) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()

			pr.Out.Header.Del("X-User-ID")
			if uid, ok := middleware.UserIDFrom(pr.In.Context()); ok {
				pr.Out.Header.Set("X-User-ID", uid)
			}
			if rid := middleware.RequestIDFrom(pr.In.Context()); rid != "" {
				pr.Out.Header.Set("X-Request-ID", rid)
			}
			// Proves to the backend that this request came through the
			// gateway (see item-service/gatewayguard.go). Never taken from
			// the client: it is read from the gateway's own environment.
			pr.Out.Header.Del("X-Gateway-Secret")
			if gatewaySecret != "" {
				pr.Out.Header.Set("X-Gateway-Secret", gatewaySecret)
			}
		},
	}
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
