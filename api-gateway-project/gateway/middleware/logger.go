// contains one function that wraps any handler and logs details about every request that passes through
package middleware

import (
	"fmt"
	"net/http"
	"time"
)

// we cant read the statuscode backward
// so we will store it in struct
type wrappedWriter struct{
	 http.ResponseWriter //embedded inside struct The embedded type’s methods and fields become directly accessible.
	status_code int
}
//method for wrappedWriter structure
//personal WriteHeader
func (w *wrappedWriter) WriteHeader (code int){
	w.status_code = code
	w.ResponseWriter.WriteHeader(code) //actual WriteHeader function
}


func Logger(next http.Handler) http.Handler{
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {  //“Create an anonymous HTTP handler function and return it.”
		start := time.Now()
		wrapped := &wrappedWriter{ResponseWriter: w,status_code: 200} //default is 200 , custom method changes it later
		next.ServeHTTP(wrapped,r)
		fmt.Printf("[LOG] %s %s | status=%d | latency=%dms\n",
            r.Method,
            r.URL.Path,
            wrapped.status_code,
            time.Since(start).Milliseconds(),
        )
	})
}