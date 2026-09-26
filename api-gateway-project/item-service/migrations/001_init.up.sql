CREATE TABLE users (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL,
  email      TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE items (
  id          BIGSERIAL PRIMARY KEY,
  seller_id   BIGINT NOT NULL REFERENCES users(id),
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  price_jpy   INTEGER NOT NULL CHECK (price_jpy > 0),
  status      TEXT NOT NULL DEFAULT 'on_sale'
              CHECK (status IN ('on_sale', 'sold', 'cancelled')),
  buyer_id    BIGINT REFERENCES users(id),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  sold_at     TIMESTAMPTZ
);
 
CREATE TABLE orders (
  id         BIGSERIAL PRIMARY KEY,
  item_id    BIGINT NOT NULL UNIQUE REFERENCES items(id),  -- one order per item
  buyer_id   BIGINT NOT NULL REFERENCES users(id),
  price_jpy  INTEGER NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
 
-- supports "newest on_sale items" with keyset pagination
CREATE INDEX idx_items_status_created ON items (status, created_at DESC, id DESC);
CREATE INDEX idx_items_seller ON items (seller_id);
