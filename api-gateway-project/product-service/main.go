package main

import (
	// "fmt"
	"net/http"
	"encoding/json"

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

	json.NewEncoder(res).Encode(products)
}

func main(){
	http.HandleFunc("/product",getProduct)
	http.ListenAndServe(":3001",nil)
}
