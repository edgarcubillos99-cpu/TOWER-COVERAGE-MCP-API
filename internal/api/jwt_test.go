package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func signHS256(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("no se pudo firmar el token de prueba: %v", err)
	}
	return signed
}

func TestValidateJWT(t *testing.T) {
	const secret = "shared-secret-entre-apis"

	valid := signHS256(t, secret, jwt.MapClaims{
		"id":  1,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err := validateJWT(valid, secret); err != nil {
		t.Errorf("token válido rechazado: %v", err)
	}

	expired := signHS256(t, secret, jwt.MapClaims{
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	if err := validateJWT(expired, secret); err == nil {
		t.Error("se esperaba error para token caducado")
	}

	noExp := signHS256(t, secret, jwt.MapClaims{"sub": "agenda"})
	if err := validateJWT(noExp, secret); err == nil {
		t.Error("se esperaba error para token sin claim exp")
	}

	wrongSecret := signHS256(t, secret, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err := validateJWT(wrongSecret, "otro-secreto"); err == nil {
		t.Error("se esperaba error para firma inválida")
	}

	if err := validateJWT("no-es-un-jwt", secret); err == nil {
		t.Error("se esperaba error para token malformado")
	}
}

func TestWithJWT(t *testing.T) {
	const secret = "shared-secret-entre-apis"

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	t.Run("sin JWT_SECRET deja el handler abierto", func(t *testing.T) {
		h := withJWT("", ok)
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, "/api/torres", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
		}
	})

	t.Run("JWT válido autentica", func(t *testing.T) {
		h := withJWT(secret, ok)
		token := signHS256(t, secret, jwt.MapClaims{
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		req := httptest.NewRequest(http.MethodGet, "/api/torres", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
		}
	})

	t.Run("sin credenciales rechaza", func(t *testing.T) {
		h := withJWT(secret, ok)
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, "/api/torres", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
		}
	})

	t.Run("JWT caducado rechaza", func(t *testing.T) {
		h := withJWT(secret, ok)
		token := signHS256(t, secret, jwt.MapClaims{
			"exp": time.Now().Add(-time.Hour).Unix(),
		})
		req := httptest.NewRequest(http.MethodGet, "/api/torres", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("esperaba 401, obtuvo %d", rec.Code)
		}
	})
}
