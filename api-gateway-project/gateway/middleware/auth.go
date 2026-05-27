package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Auth takes a SECRET, returns a function that takes a handler, returns a handler
func Auth(secret string) func(http.Handler) http.Handler{
	 return func(next http.Handler) http.Handler {return http.HandlerFunc(func (w http.ResponseWriter,r *http.Request)  {
		//checkin if header exist
		authHeader := r.Header.Get("Authorization")
		if authHeader == ""{
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// taking the token
		parts := strings.SplitN(authHeader," ",2)
		if len(parts)!=2 || parts[0] != "Bearer" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}		
		tokenstring := parts[1]

		token,err := jwt.Parse(tokenstring,func (t *jwt.Token)(interface{},error)  {
			return []byte(secret),nil
		})
		if err != nil || !token.Valid {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}	

		claims := token.Claims.(jwt.MapClaims)

		user_id := claims["user_id"].(string)
		// Because the Rate Limiter (which comes next in the chain) needs the user ID

		// Context is the solution. Every HTTP request carries a small key-value bag with it. You can put things in, and the next handler can take them out.
		ctx := context.WithValue(r.Context(),"user_id",user_id)
		r = r.WithContext(ctx)
		next.ServeHTTP(w,r)

	})}
}