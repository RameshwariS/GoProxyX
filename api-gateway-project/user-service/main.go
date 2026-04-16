package main

import (
	// "fmt"
	"net/http"
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
	http.HandleFunc("/user",user_func)
	http.HandleFunc("/users",user_func)
	http.HandleFunc("/users/",user_func)
	http.ListenAndServe(":3002",nil)
}
