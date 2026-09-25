package apsync

import (
	"testing"

	"tower-scraper/internal/db"
)

func TestLookupDispID(t *testing.T) {
	idx := indexDispositivosByNameIP([]db.DispositivoCatalogo{
		{ID: 42, Dispositivo: "OSNAP16-A", IPAddress: "10.0.0.16"},
		{ID: 7, Dispositivo: "cap2", IPAddress: " 10.8.8.8 "},
		{ID: 931, Dispositivo: "OSNAP16-A ePMP3000", IPAddress: "10.245.16.2"},
		{ID: 946, Dispositivo: "OSNAP16-N (Rocket 5AC Lite)", IPAddress: "10.245.16.15"},
		{ID: 481, Dispositivo: "OSNAP164-A ePMP3000L", IPAddress: "10.245.164.2"},
		{ID: 938, Dispositivo: "OSNAP16-H ePMP3000", IPAddress: "10.245.16.9"},
		{ID: 937, Dispositivo: "OSNAP16-H ePMP 3000", IPAddress: "10.245.16.9"},
	})
	id, ok := lookupDispID(idx, "osnap16-a", "10.0.0.16")
	if !ok || id != 42 {
		t.Fatalf("exact: id=%d ok=%t", id, ok)
	}
	id, ok = lookupDispID(idx, "CAP2", "10.8.8.8")
	if !ok || id != 7 {
		t.Fatalf("case/trim: id=%d ok=%t", id, ok)
	}
	id, ok = lookupDispID(idx, "OSNAP16-A", "10.245.16.2")
	if !ok || id != 931 {
		t.Fatalf("nombre con tipo: id=%d ok=%t", id, ok)
	}
	id, ok = lookupDispID(idx, "OSNAP16-N", "10.245.16.15")
	if !ok || id != 946 {
		t.Fatalf("nombre con paréntesis: id=%d ok=%t", id, ok)
	}
	if _, ok := lookupDispID(idx, "OSNAP16-A", "10.245.164.2"); ok {
		t.Fatal("OSNAP16-A no debe casar con OSNAP164-A")
	}
	id, ok = lookupDispID(idx, "OSNAP16-H", "10.245.16.9")
	if !ok || id != 937 {
		t.Fatalf("duplicado: se espera id más bajo, got %d ok=%t", id, ok)
	}
	if _, ok := lookupDispID(idx, "OSNAP16-A", ""); ok {
		t.Fatal("sin IP no debe matchear")
	}
	if _, ok := lookupDispID(idx, "OSNAP16-A", "9.9.9.9"); ok {
		t.Fatal("IP distinta no debe matchear")
	}
}
