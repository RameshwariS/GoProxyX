package middleware

import "net/http"

// CORS allows a browser-based frontend hosted on a different origin (as it
// will be on Render: the frontend is a static site with its own
// onrender.com domain, separate from the gateway's) to call this API.
//
// It reflects "*" (any origin) rather than a specific allow-listed one,
// which keeps local development and preview deploys working without
// reconfiguration. This is safe here because the API is authenticated with
// a Bearer token in a header, not a cookie -- there is no session for a
// malicious page to ride on, which is the actual risk "*" would otherwise
// introduce. If this project later added cookie-based auth, this would
// need to become a specific allow-listed origin instead.
//
// CORS must wrap the entire mux, outside RequestID/Auth/everything else:
// a browser's preflight OPTIONS request never carries an Authorization
// header, so if Auth ran first, every preflight would be rejected with 401
// and the browser would never even attempt the real request.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-RateLimit-Limit, X-RateLimit-Remaining, Retry-After")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
