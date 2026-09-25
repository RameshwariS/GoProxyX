// contains one function that wraps any handler and logs details about every request that passes through
package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// we cant read the statuscode backward
// so we will store it in struct
type wrappedWriter struct {
	http.ResponseWriter //embedded inside struct The embedded type’s methods and fields become directly accessible.
	status_code         int
	wroteHeader         bool
}

// method for wrappedWriter structure
// personal WriteHeader
func (w *wrappedWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status_code = code
	w.ResponseWriter.WriteHeader(code) //actual WriteHeader function
}

func (w *wrappedWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

// Unwrap preserves optional ResponseWriter features for net/http helpers such
// as ReverseProxy and ResponseController.
func (w *wrappedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //“Create an anonymous HTTP handler function and return it.”
		start := time.Now()
		wrapped := &wrappedWriter{ResponseWriter: w, status_code: 200} //default is 200 , custom method changes it later
		next.ServeHTTP(wrapped, r)
		slog.Info("http request", "method", r.Method, "path", r.URL.Path,
			"status", wrapped.status_code, "latency_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFrom(r.Context()))
	})
}
