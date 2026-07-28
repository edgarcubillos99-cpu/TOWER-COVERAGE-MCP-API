// Script de utilidad para generar JWT de prueba firmados con JWT_SECRET.
// Uso: JWT_SECRET=tu-secreto go run scripts/genjwt.go
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "Define JWT_SECRET antes de ejecutar, por ejemplo:")
		fmt.Fprintln(os.Stderr, "  JWT_SECRET=tu-secreto go run scripts/genjwt.go")
		os.Exit(1)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "swagger-test",
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	})

	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error firmando JWT: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(signed)
}
