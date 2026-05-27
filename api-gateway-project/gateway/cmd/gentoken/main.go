package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	userID := flag.String("user", "", "user id to place in the token")
	secret := flag.String("secret", "my_key", "JWT signing secret")
	expiresIn := flag.Duration("expires", 24*time.Hour, "token lifetime")
	flag.Parse()

	if *userID == "" {
		log.Fatal("missing required --user value")
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
