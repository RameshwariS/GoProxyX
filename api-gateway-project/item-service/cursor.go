package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A cursor identifies a position in the "on_sale items, newest first" order
// by the (created_at, id) pair of the last item the caller already saw --
// the same pair ListItems' WHERE clause compares against. Encoding it as
// "<RFC3339Nano timestamp>_<id>" keeps it a single opaque string in the API
// (a query parameter and a JSON field), while still being exactly the two
// values the SQL keyset condition needs.
func encodeCursor(createdAt time.Time, id int64) string {
	return createdAt.Format(time.RFC3339Nano) + "_" + strconv.FormatInt(id, 10)
}

func decodeCursor(raw string) (sql.NullTime, sql.NullInt64, error) {
	if raw == "" {
		return sql.NullTime{}, sql.NullInt64{}, nil
	}
	ts, idPart, found := strings.Cut(raw, "_")
	if !found {
		return sql.NullTime{}, sql.NullInt64{}, fmt.Errorf("cursor missing separator")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return sql.NullTime{}, sql.NullInt64{}, fmt.Errorf("bad cursor timestamp: %w", err)
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		return sql.NullTime{}, sql.NullInt64{}, fmt.Errorf("bad cursor id: %w", err)
	}
	return sql.NullTime{Time: t, Valid: true}, sql.NullInt64{Int64: id, Valid: true}, nil
}
