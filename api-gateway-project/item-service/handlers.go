package main
 
import (
  "database/sql"
  "encoding/json"
  "errors"
  "net/http"
  "strconv"
  "strings"
)
 
type Handlers struct {
  store *Store
}
 

func writeError(w http.ResponseWriter, status int, code, msg string) {
  w.Header().Set("Content-Type", "application/json")
  w.WriteHeader(status)
  _ = json.NewEncoder(w).Encode(map[string]any{
    "error": map[string]string{"code": code, "message": msg},
  })
}
 
func callerID(r *http.Request) (int64, bool) {
  v := r.Header.Get("X-User-ID")
  id, err := strconv.ParseInt(v, 10, 64)
  return id, err == nil
}
 
func (h *Handlers) ListItems(w http.ResponseWriter, r *http.Request) {
  q := r.URL.Query().Get("q")
  limit := 20
  if v := r.URL.Query().Get("limit"); v != "" {
    if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
      limit = n
    }
  }
  items, err := h.store.ListItems(r.Context(), q, limit, sql.NullTime{}, sql.NullInt64{})
  if err != nil {
    writeError(w, http.StatusInternalServerError, "internal_error", "could not list items")
    return
  }
  if items == nil {
    items = []Item{}
  }
  json.NewEncoder(w).Encode(items)
}
 
func (h *Handlers) GetItem(w http.ResponseWriter, r *http.Request, id int64) {
  item, err := h.store.GetItem(r.Context(), id)
  switch {
  case errors.Is(err, ErrNotFound):
    writeError(w, http.StatusNotFound, "not_found", "item not found")
  case err != nil:
    writeError(w, http.StatusInternalServerError, "internal_error", "could not fetch item")
  default:
    json.NewEncoder(w).Encode(item)
  }
}
 
type createItemRequest struct {
  Title       string `json:"title"`
  Description string `json:"description"`
  PriceJPY    int    `json:"price_jpy"`
}
 
func (h *Handlers) CreateItem(w http.ResponseWriter, r *http.Request) {
  sellerID, ok := callerID(r)
  if !ok {
    writeError(w, http.StatusUnauthorized, "unauthenticated", "missing caller identity")
    return
  }
 
  var req createItemRequest
  if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
    writeError(w, http.StatusBadRequest, "invalid_body", "could not parse request body")
    return
  }
  if strings.TrimSpace(req.Title) == "" || req.PriceJPY <= 0 {
    writeError(w, http.StatusBadRequest, "invalid_body", "title and a positive price_jpy are required")
    return
  }
 
  item, err := h.store.CreateItem(r.Context(), sellerID, req.Title, req.Description, req.PriceJPY)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "internal_error", "could not create item")
    return
  }
  w.WriteHeader(http.StatusCreated)
  json.NewEncoder(w).Encode(item)
}
 
func (h *Handlers) Purchase(w http.ResponseWriter, r *http.Request, id int64) {
  buyerID, ok := callerID(r)
  if !ok {
    writeError(w, http.StatusUnauthorized, "unauthenticated", "missing caller identity")
    return
  }
 
  order, err := h.store.Purchase(r.Context(), id, buyerID)
  switch {
  case errors.Is(err, ErrNotFound):
    writeError(w, http.StatusNotFound, "not_found", "item not found")
  case errors.Is(err, ErrAlreadySold):
    writeError(w, http.StatusConflict, "already_sold", "item is no longer for sale")
  case errors.Is(err, ErrOwnItem):
    writeError(w, http.StatusForbidden, "own_item", "you cannot buy your own item")
  case err != nil:
    writeError(w, http.StatusInternalServerError, "internal_error", "could not complete purchase")
  default:
    json.NewEncoder(w).Encode(order)
  }
}
