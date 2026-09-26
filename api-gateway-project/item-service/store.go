package main

import(
	"context"
	"database/sql"
	"errors"
)

var(
	ErrNotFound    = errors.New("item not found")
  ErrAlreadySold = errors.New("item already sold")
  ErrOwnItem     = errors.New("cannot buy your own item")

)

type Store struct {
  db *sql.DB
}
 
func NewStore(db *sql.DB) *Store {
  return &Store{db: db}
}

