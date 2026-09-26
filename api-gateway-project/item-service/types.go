package main

import "time"

type Item struct {
  ID          int64      `json:"id"`
  SellerID    int64      `json:"seller_id"`
  Title       string     `json:"title"`
  Description string     `json:"description"`
  PriceJPY    int        `json:"price_jpy"`
  Status      string     `json:"status"`
  BuyerID     *int64     `json:"buyer_id,omitempty"`
  CreatedAt   time.Time  `json:"created_at"`
  SoldAt      *time.Time `json:"sold_at,omitempty"`
}
 
type Order struct {
  ID        int64     `json:"id"`
  ItemID    int64     `json:"item_id"`
  BuyerID   int64     `json:"buyer_id"`
  PriceJPY  int       `json:"price_jpy"`
  CreatedAt time.Time `json:"created_at"`
}
