package main

import (
	// "fmt"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type Product struct{
	Id int `json:"id"`
	Name string `json:"name"`

}

func getProduct(res http.ResponseWriter,req *http.Request){
	products := []Product{
		{Id: 1,Name: "Phone"},
		{Id: 2,Name:"Laptop"},
	}
	path := req.URL.Path
	if path == "/products"{
		json.NewEncoder(res).Encode(products)
	}else{
		id,err := strconv.Atoi(strings.TrimPrefix(path,"/products/"))
		if err != nil {
			// fmt.Println(id)
			res.WriteHeader(400)
			return
		}
		for _,p := range products {
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

	http.HandleFunc("/products",getProduct)
	http.HandleFunc("/products/",getProduct)
	http.ListenAndServe(":3001",nil)
}
