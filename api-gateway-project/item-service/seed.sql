-- Run after migrations/001_init.up.sql. Safe to run repeatedly.
BEGIN;

-- The items table has a foreign key to users, so seed sellers first.
INSERT INTO users (name, email)
VALUES
  ('Aarav Sharma', 'aarav.sharma@example.com'),
  ('Priya Verma', 'priya.verma@example.com'),
  ('Rohan Mehta', 'rohan.mehta@example.com')
ON CONFLICT (email) DO NOTHING;

-- Add sample items only if the same seller/title pair is not already present.
INSERT INTO items (seller_id, title, description, price_jpy)
SELECT u.id, seed.title, seed.description, seed.price_jpy
FROM (VALUES
  ('aarav.sharma@example.com', 'Used bicycle', 'A well-maintained city bicycle.', 12000),
  ('aarav.sharma@example.com', 'Desk lamp', 'Adjustable LED desk lamp.', 2500),
  ('priya.verma@example.com', 'Coffee maker', 'Compact drip coffee maker in good condition.', 4800),
  ('rohan.mehta@example.com', 'Mechanical keyboard', 'Tenkeyless keyboard with tactile switches.', 8500)
) AS seed(seller_email, title, description, price_jpy)
JOIN users AS u ON u.email = seed.seller_email
WHERE NOT EXISTS (
  SELECT 1
  FROM items AS existing
  WHERE existing.seller_id = u.id
    AND existing.title = seed.title
);

COMMIT;
