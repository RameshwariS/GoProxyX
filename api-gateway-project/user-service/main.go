package main

import (
	// "fmt"
	"net/http"
	"os"
	"encoding/json"
	"strconv"
	"strings"

)

type User struct{
	Id int `json:"id"`
	Name string `json:"name"`

}

func user_func(res http.ResponseWriter,req *http.Request){
	users := []User{
		{Id: 1,Name: "Alice"},
		{Id: 2,Name:"Bob"},
	}

	path := req.URL.Path
	if path == "/users"{
		json.NewEncoder(res).Encode(users)
	}else{
		id,err := strconv.Atoi(strings.TrimPrefix(path,"/users/"))
		if err != nil {
			res.WriteHeader(400)
			return
		}
		for _,u := range users {
			if u.Id ==  id {
				json.NewEncoder(res).Encode(u)
				return
			}
		}
		http.NotFound(res,req)

	}

}

func main(){
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/user",user_func)
	mux.HandleFunc("/users",user_func)
	mux.HandleFunc("/users/",user_func)

	// PORT is injected by Render (web services must bind it); default 3002
	// matches docker-compose.yml for local development.
	port := os.Getenv("PORT")
	if port == "" {
		port = "3002"
	}
	http.ListenAndServe(":"+port, requireGatewaySecret(mux))
}
