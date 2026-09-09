package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// errInvalidToken se devuelve cuando el JWT no es válido: firma incorrecta,
// algoritmo inesperado, token caducado o sin el claim "exp".
var errInvalidToken = errors.New("token JWT inválido o caducado")

// validateJWT verifica la firma HS256 de un JWT contra el secreto compartido
// entre las APIs de la empresa y comprueba que no haya caducado (claim "exp").
func validateJWT(tokenString, secret string) error {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return errInvalidToken
	}
	return nil
}

// bearerJWTFromRequest extrae el token del header "Authorization: Bearer <jwt>".
// Si el valor incluye "Bearer " duplicado (p. ej. pegado así en Swagger), lo normaliza.
func bearerJWTFromRequest(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if strings.HasPrefix(token, prefix) {
		token = strings.TrimSpace(strings.TrimPrefix(token, prefix))
	}
	if token == "" {
		return "", false
	}
	return token, true
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="Tower Coverage API", charset="UTF-8"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "Unauthorized: JWT inválido, caducado o ausente",
	})
}

// withJWT exige en /api/* un JWT válido (Authorization: Bearer <jwt> firmado con
// jwtSecret). Si jwtSecret está vacío, el handler queda abierto (solo desarrollo).
func withJWT(jwtSecret string, next http.HandlerFunc) http.HandlerFunc {
	if jwtSecret == "" {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerJWTFromRequest(r)
		if !ok {
			writeUnauthorized(w)
			return
		}
		if err := validateJWT(token, jwtSecret); err != nil {
			writeUnauthorized(w)
			return
		}
		next(w, r)
	}
}
