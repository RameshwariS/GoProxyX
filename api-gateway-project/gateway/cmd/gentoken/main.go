package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	userID := flag.String("user", "", "user id to place in the token")
	secret := flag.String("secret", "", "JWT signing secret (defaults to $JWT_SECRET)")
	expiresIn := flag.Duration("expires", 24*time.Hour, "token lifetime")
	flag.Parse()

	if *userID == "" {
		log.Fatal("missing required --user value")
	}

	// No hardcoded fallback: this must match the real gateway's JWT_SECRET,
	// or every token this tool prints will fail verification with
	// "invalid_token" and give no hint why.
	if *secret == "" {
		*secret = os.Getenv("JWT_SECRET")
	}
	if *secret == "" {
		log.Fatal("missing secret: pass --secret or set the JWT_SECRET environment variable (it must match the gateway's)")
	}

	claims := jwt.MapClaims{
		"user_id": *userID,
		"exp":     time.Now().Add(*expiresIn).Unix(),
		"iat":     time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(*secret))
	if err != nil {
		log.Fatalf("sign token: %v", err)
	}

	fmt.Println(signedToken)
}
