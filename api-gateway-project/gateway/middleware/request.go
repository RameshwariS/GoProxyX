package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
)

type requestIDKey struct{}

// RequestID assigns every request an unpredictable identifier and returns it
// in the response so clients can correlate failures with gateway logs.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			slog.Error("could not generate request ID", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		id := hex.EncodeToString(raw[:])
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type recoverWriter struct {
	http.ResponseWriter
	committed bool
}

func (w *recoverWriter) WriteHeader(status int) {
	if w.committed {
		return
	}
	w.committed = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *recoverWriter) Write(body []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *recoverWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Recover turns handler panics into a 500 response when the response has not
// already started, while keeping the server process alive.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := &recoverWriter{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("http handler panic", "request_id", RequestIDFrom(r.Context()), "panic", recovered)
				if !wrapped.committed {
					WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
				}
			}
		}()
		next.ServeHTTP(wrapped, r)
	})
}
