#!/bin/sh
# Runs before the item-service binary starts, on every container start
# (fresh deploy or restart). Both SQL files are idempotent (migrations use
# IF NOT EXISTS, seed.sql uses ON CONFLICT DO NOTHING / a NOT EXISTS guard),
# so running them again on an already-initialized database is always safe
# -- this is what lets a Render deploy be "push and it just works" instead
# of "push, then remember to SSH in and run migrations by hand."
set -e

if [ -z "$DATABASE_URL" ]; then
  echo "entrypoint: DATABASE_URL is not set" >&2
  exit 1
fi

echo "entrypoint: waiting for postgres..."
attempt=0
until psql "$DATABASE_URL" -c '\q' >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "entrypoint: postgres did not become reachable in time" >&2
    exit 1
  fi
  sleep 1
done

echo "entrypoint: applying migrations..."
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f /app/migrations/001_init.up.sql

echo "entrypoint: applying seed data..."
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f /app/seed.sql

echo "entrypoint: starting item-service"
exec /app/item-service
