package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey string // named type with underlying type string
const userIDKey ctxKey = "user_id"

// UserIDFrom is the only way other packages read the caller identity.
func UserIDFrom(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDKey).(string)
	return v, ok && v != ""
}

//getting the token

func bearerToken(r *http.Request) (string, bool) {
	_, tok, found := strings.Cut(r.Header.Get("Authorization"), " ")

	if !found || tok == "" {
		return "", false
	}
	return tok, true
}

// Auth takes a SECRET, returns a function that takes a handler, returns a handler
func Auth(secret string) func(http.Handler) http.Handler {
	key := []byte(secret)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//checkin if header exist
			raw, ok := bearerToken(r)
			if !ok {
				WriteError(w, 401, "missing_token", "Authorization: Bearer <token> required")
				return
			}

			claims := jwt.MapClaims{}

			tok, err := jwt.ParseWithClaims(raw, claims,
				func(*jwt.Token) (any, error) { return key, nil },
				jwt.WithValidMethods([]string{"HS256"}),
				jwt.WithExpirationRequired(),
			)

			if err != nil || !tok.Valid {
				WriteError(w, 401, "invalid_token", "token invalid or expired")
				return
			}
			uid, ok := claims["user_id"].(string)

			if !ok || uid == "" {
				WriteError(w, 401, "invalid_token", "token missing user_id")
				return

			}

			ctx := context.WithValue(r.Context(), userIDKey, uid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func WriteError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}
