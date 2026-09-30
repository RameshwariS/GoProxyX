package main
 
import (
  "context"
  "database/sql"
  "log/slog"
  "net/http"
  "os"
  "strconv"
  "strings"
  "time"
 
  _ "github.com/lib/pq"
)
 
func main() {
  slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
 
  dsn := os.Getenv("DATABASE_URL")
  if dsn == "" {
    slog.Error("missing DATABASE_URL")
    os.Exit(1)
  }
 
  db, err := sql.Open("postgres", dsn)
  if err != nil {
    slog.Error("sql.Open failed", "err", err)
    os.Exit(1)
  }
  defer db.Close()
 
  db.SetMaxOpenConns(10)
  db.SetMaxIdleConns(5)
  db.SetConnMaxLifetime(30 * time.Minute)
 
  pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
  defer cancel()
  if err := db.PingContext(pingCtx); err != nil {
    slog.Error("database not reachable", "err", err)
    os.Exit(1)
  }
  slog.Info("connected to database")
 
  store := NewStore(db)
  h := &Handlers{store: store}
 
  mux := http.NewServeMux()
 
  mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
  })
 
  mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
      h.ListItems(w, r)
    case http.MethodPost:
      h.CreateItem(w, r)
    default:
      w.WriteHeader(http.StatusMethodNotAllowed)
    }
  })
 
  mux.HandleFunc("/items/", func(w http.ResponseWriter, r *http.Request) {
    rest := strings.TrimPrefix(r.URL.Path, "/items/")
    parts := strings.Split(rest, "/")
 
    id, err := strconv.ParseInt(parts[0], 10, 64)
    if err != nil {
      writeError(w, http.StatusBadRequest, "invalid_id", "item id must be a number")
      return
    }
 
    switch {
    case len(parts) == 1 && r.Method == http.MethodGet:
      h.GetItem(w, r, id)
    case len(parts) == 2 && parts[1] == "purchase" && r.Method == http.MethodPost:
      h.Purchase(w, r, id)
    default:
      w.WriteHeader(http.StatusNotFound)
    }
  })
 
  // PORT is injected by Render (web services must bind it); default 3001
  // matches docker-compose.yml for local development.
  port := os.Getenv("PORT")
  if port == "" {
    port = "3001"
  }
  srv := &http.Server{
    Addr:              ":" + port,
    Handler:           requireGatewaySecret(mux),
    ReadHeaderTimeout: 5 * time.Second,
  }
  slog.Info("item-service starting", "addr", srv.Addr)
  if err := srv.ListenAndServe(); err != nil {
    slog.Error("server error", "err", err)
    os.Exit(1)
  }
}
