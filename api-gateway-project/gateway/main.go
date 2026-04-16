package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)
func health_handler(res http.ResponseWriter, req *http.Request){
	res.WriteHeader(http.StatusOK)	
}

func handler(res http.ResponseWriter,req *http.Request){
	path := req.URL.Path

	if strings.HasPrefix(path,"/users") {
		//proxy to user service
		target,_ := url.Parse("http://user-service:3002") // converts string to url object
		proxy := httputil.NewSingleHostReverseProxy(target) // creates reverse proxy object
		proxy.ServeHTTP(res,req)
		return
		
	}else if strings.HasPrefix(path,"/products") {
		//proxy to product service
		target,_ := url.Parse("http://product-service:3001")
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.ServeHTTP(res,req)
		return
	}else if path == "/health" {	
		res.WriteHeader(http.StatusOK)	
	}else {
		res.WriteHeader(404)
		fmt.Fprintf(res,"Not Found")
	}
	
}

func main(){
	http.HandleFunc("/health",health_handler)

	// will take the input from the request and then check where to send
	http.HandleFunc("/",handler)

	http.ListenAndServe(":3000",nil)
}
