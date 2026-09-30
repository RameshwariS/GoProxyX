package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// demoUsers mirrors the sellers item-service/seed.sql creates in Postgres
// (BIGSERIAL ids are assigned in insertion order on a fresh database, so
// these ids are stable: Aarav is always 1, Priya always 2, Rohan always 3).
// This list only drives the login screen's choices; the login endpoint
// itself will mint a token for any user id you send it.
var demoUsers = []struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}{
	{1, "Aarav Sharma"},
	{2, "Priya Verma"},
	{3, "Rohan Mehta"},
}

type loginRequest struct {
	UserID string `json:"user_id"`
}

type loginResponse struct {
	Token string `json:"token"`
	User  struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
}

// authHandlers holds the one piece of state a login handler needs: the
// secret to sign new tokens with. It intentionally has no dependency on
// Redis, Postgres, or any other service, so login keeps working even if
// something downstream is degraded.
type authHandlers struct {
	secret string
}

// listDemoUsers handles GET /auth/demo-users -- lets the frontend populate
// its login screen without hardcoding names on both ends.
func (a *authHandlers) listDemoUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(demoUsers)
}

// login handles POST /auth/login. This is a deliberately simplified,
// unauthenticated "sign in" endpoint for a portfolio demo: it mints a
// signed JWT for whatever user_id is sent, the same way cmd/gentoken does
// from the command line, but reachable over HTTP so a browser-based
// frontend can use it. A real system would verify a password or an OAuth
// token here before issuing anything -- see the comment above demoUsers
// for why the ids in this project's login screen are meaningful (they
// must exist in Postgres for CreateItem/Purchase to succeed, since
// seller_id/buyer_id are foreign keys).
func (a *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "user_id is required")
		return
	}

	claims := jwt.MapClaims{
		"user_id": req.UserID,
		"iat":     time.Now().Unix(),
		"exp":     time.Now().Add(12 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(a.secret))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "could not sign token")
		return
	}

	var res loginResponse
	res.Token = signed
	if id, err := strconv.ParseInt(req.UserID, 10, 64); err == nil {
		res.User.ID = id
		for _, u := range demoUsers {
			if u.ID == id {
				res.User.Name = u.Name
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}
