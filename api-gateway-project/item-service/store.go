package main

import(
	"context"
	"database/sql"
	"errors"
)

 //allows a database query to be cancelled if the request is cancelled or times out
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
//q user input query string, limit number of items to return, afterCreated and afterID for pagination
//pagination means dividing into chunks

func (s *Store) ListItems(ctx context.Context, q string, limit int, afterCreated sql.NullTime, afterID sql.NullInt64) ([]Item, error) { // function belong to Store struct
	query := `  SELECT id, seller_id, title, description, price_jpy, status, buyer_id, created_at, sold_at
    FROM items
    WHERE status = 'on_sale'
      AND ($1 = '' OR title ILIKE '%' || $1 || '%')
      AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
    ORDER BY created_at DESC, id DESC
    LIMIT $4`
	rows,err := s.db.QueryContext(ctx,query,q,afterCreated,afterID,limit)
	if(err != nil){
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
    var it Item
	// take values returned by the database and put them into Go variables.
    if err := rows.Scan(&it.ID, &it.SellerID, &it.Title, &it.Description,
      &it.PriceJPY, &it.Status, &it.BuyerID, &it.CreatedAt, &it.SoldAt); err != nil {
      return nil, err
    }
    items = append(items, it)
  }
  return items, rows.Err()

}

func (s *Store) GetItem(ctx context.Context, id int64) (Item, error) {
  var it Item
  err := s.db.QueryRowContext(ctx, `
    SELECT id, seller_id, title, description, price_jpy, status, buyer_id, created_at, sold_at
    FROM items WHERE id = $1`, id,
  ).Scan(&it.ID, &it.SellerID, &it.Title, &it.Description,
    &it.PriceJPY, &it.Status, &it.BuyerID, &it.CreatedAt, &it.SoldAt)
 
  if errors.Is(err, sql.ErrNoRows) {
    return Item{}, ErrNotFound
  }
  return it, err
}
 
func (s *Store) CreateItem(ctx context.Context, sellerID int64, title, description string, priceJPY int) (Item, error) {
  var it Item
  err := s.db.QueryRowContext(ctx, `
    INSERT INTO items (seller_id, title, description, price_jpy)
    VALUES ($1, $2, $3, $4)
    RETURNING id, seller_id, title, description, price_jpy, status, buyer_id, created_at, sold_at`,
    sellerID, title, description, priceJPY,
  ).Scan(&it.ID, &it.SellerID, &it.Title, &it.Description,
    &it.PriceJPY, &it.Status, &it.BuyerID, &it.CreatedAt, &it.SoldAt)
  return it, err
}

func (s *Store) Purchase(ctx context.Context, itemID, buyerID int64) (Order, error) {
  tx, err := s.db.BeginTx(ctx, nil)
  if err != nil {
    return Order{}, err
  }
  defer tx.Rollback()
 
  var status string
  var sellerID int64
  var price int
  err = tx.QueryRowContext(ctx,
    `SELECT status, seller_id, price_jpy FROM items WHERE id = $1 FOR UPDATE`,
    itemID,
  ).Scan(&status, &sellerID, &price)
 
  if errors.Is(err, sql.ErrNoRows) {
    return Order{}, ErrNotFound
  }
  if err != nil {
    return Order{}, err
  }
  if sellerID == buyerID {
    return Order{}, ErrOwnItem
  }
  if status != "on_sale" {
    return Order{}, ErrAlreadySold
  }
 
  if _, err = tx.ExecContext(ctx,
    `UPDATE items SET status = 'sold', buyer_id = $2, sold_at = now() WHERE id = $1`,
    itemID, buyerID,
  ); err != nil {
    return Order{}, err
  }
 
  var o Order
  err = tx.QueryRowContext(ctx, `
    INSERT INTO orders (item_id, buyer_id, price_jpy)
    VALUES ($1, $2, $3)
    RETURNING id, item_id, buyer_id, price_jpy, created_at`,
    itemID, buyerID, price,
  ).Scan(&o.ID, &o.ItemID, &o.BuyerID, &o.PriceJPY, &o.CreatedAt)
  if err != nil {
    return Order{}, err
  }
 
  return o, tx.Commit()
}

