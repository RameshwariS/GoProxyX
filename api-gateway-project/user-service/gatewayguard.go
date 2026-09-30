package main

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
)

// requireGatewaySecret makes this service refuse any request that did not
// come through the API gateway. See item-service/gatewayguard.go for the
// full reasoning; this is the same check for the same reason.
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"code": "not_via_gateway", "message": "this service only accepts requests from the gateway"},
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
