package main

import (
	"net/http"
)
func health_handler(res http.ResponseWriter, req *http.Request){
	res.WriteHeader(http.StatusOK)	
}
func main(){
	http.HandleFunc("/health",health_handler)

	http.ListenAndServe(":3000",nil)
}