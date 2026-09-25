package main

import (
	// "fmt"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type Item struct{
	Id int `json:"id"`
	Name string `json:"name"`

}

func getItem(res http.ResponseWriter,req *http.Request){
	items := []Item{
		{Id: 1,Name: "Phone"},
		{Id: 2,Name:"Laptop"},
	}
	path := req.URL.Path
	if path == "/items"{
		json.NewEncoder(res).Encode(items)
	}else{
		id,err := strconv.Atoi(strings.TrimPrefix(path,"/items/"))
		if err != nil {
			// fmt.Println(id)
			res.WriteHeader(400)
			return
		}
		for _,p := range items {
			if p.Id ==  id {
				json.NewEncoder(res).Encode(p)
				return
			}
		}
		http.NotFound(res,req)

	}

	// json.NewEncoder(res).Encode(products)
}

func main(){

	http.HandleFunc("/items",getItem)
	http.HandleFunc("/items/",getItem)
	http.ListenAndServe(":3001",nil)
}
