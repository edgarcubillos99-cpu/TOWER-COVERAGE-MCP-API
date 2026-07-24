package coverage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"tower-scraper/internal/models"
)

func TestParseLatLon(t *testing.T) {
	lat, lon, err := parseLatLon("18.450123", " -66.736353 ")
	if err != nil {
		t.Fatalf("parseLatLon: %v", err)
	}
	if lat != 18.450123 || lon != -66.736353 {
		t.Fatalf("got lat=%v lon=%v", lat, lon)
	}
	if _, _, err := parseLatLon("x", "-66"); err == nil {
		t.Fatal("esperaba error con lat inválida")
	}
}

func TestResultStoreNilIsNoop(t *testing.T) {
	var store *ResultStore
	cached, dist, err := store.FindNearest(context.Background(), 18.45, -66.73)
	if err != nil || cached != nil || dist != 0 {
		t.Fatalf("nil store FindNearest: cached=%v dist=%v err=%v", cached, dist, err)
	}
	if err := store.Save(context.Background(), "18.45", "-66.73", nil, nil); err != nil {
		t.Fatalf("nil store Save: %v", err)
	}

	empty := NewResultStore(nil)
	if empty.RadiusM() != defaultCacheRadiusM {
		t.Fatalf("radius default: got %v", empty.RadiusM())
	}
	cached, _, err = empty.FindNearestFromStrings(context.Background(), "18.45", "-66.73")
	if err != nil || cached != nil {
		t.Fatalf("rdb nil FindNearest: cached=%v err=%v", cached, err)
	}
}

func TestTowersToLight(t *testing.T) {
	in := []models.TowerCoverage{{
		TowerName: "OSN.Test",
		Latitude:  "18.1",
		Longitude: "-66.2",
		Status:    "Possible",
		Signal:    "-70",
		Distance:  "1.2",
	}}
	out := towersToLight(in)
	if len(out) != 1 || out[0].TowerName != "OSN.Test" {
		t.Fatalf("towersToLight: %+v", out)
	}
	if out[0].Performance.Status != "Possible" {
		t.Fatalf("status: %q", out[0].Performance.Status)
	}
}

func TestCachedCoverageJSONRoundTrip(t *testing.T) {
	orig := CachedCoverage{
		Lat: 18.45,
		Lon: -66.73,
		Towers: []models.CoverageLightItem{{
			TowerName: "OSN.A",
			PathImage: "abc",
		}},
		Resultados: []models.RespuestaMCP{{
			Antena:      "AP1",
			Tipo:        "ubiquiti",
			Distancia:   1.5,
			Cobertura:   true,
			NombreTorre: "OSN.A",
		}},
		CachedAt: time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got CachedCoverage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Lat != orig.Lat || got.Lon != orig.Lon {
		t.Fatalf("coords: %+v", got)
	}
	if len(got.Towers) != 1 || got.Towers[0].TowerName != "OSN.A" {
		t.Fatalf("towers: %+v", got.Towers)
	}
	if len(got.Resultados) != 1 || !got.Resultados[0].Cobertura {
		t.Fatalf("resultados: %+v", got.Resultados)
	}
}
