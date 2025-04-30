package main

import (
	"log"
	"net/http"

	"github.com/EmmanuelKeifala/go-practice/bookstore/pkg/routes"
	"github.com/gorilla/mux"
)

func main() {
	r := mux.NewRouter()

	routes.RegisterBookStroreRoutes(r)

	http.Handle("/", r)
	log.Fatal(http.ListenAndServe("localhost:9010", r))
}
