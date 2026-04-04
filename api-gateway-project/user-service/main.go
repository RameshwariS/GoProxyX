package main

import (
	// "fmt"
	"net/http"
	"encoding/json"

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

	json.NewEncoder(res).Encode(users)
}

func main(){
	http.HandleFunc("/user",user_func)
	http.ListenAndServe(":3002",nil)
}
