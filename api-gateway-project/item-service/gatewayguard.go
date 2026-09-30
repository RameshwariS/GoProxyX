package main

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"os"
)

// requireGatewaySecret makes this service refuse any request that did not
// come through the API gateway.
//
// Why this exists: handlers.go trusts the X-User-ID header as the caller's
// identity, which is only safe if nobody can reach this service directly.
// Locally (Docker Compose) and on Render's paid private services, the
// network guarantees that. On Render's free plan, private services are not
// available, so this service is a public web service with a public URL --
// and without this check, anyone could call it with a made-up X-User-ID.
//
// The gateway sends a shared secret in X-Gateway-Secret on every proxied
// request; this middleware rejects everything else. /health stays open so
// the platform's health checks still work.
//
// If GATEWAY_SECRET is unset the check is skipped (and a warning is
// logged), which keeps a bare `go run` for local development frictionless.
func requireGatewaySecret(next http.Handler) http.Handler {
	secret := os.Getenv("GATEWAY_SECRET")
	if secret == "" {
		slog.Warn("GATEWAY_SECRET is not set: this service will accept requests from anyone, not only the gateway")
		return next
	}
	want := []byte(secret)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		got := []byte(r.Header.Get("X-Gateway-Secret"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			writeError(w, http.StatusUnauthorized, "not_via_gateway", "this service only accepts requests from the gateway")
			return
		}
		next.ServeHTTP(w, r)
	})
}
