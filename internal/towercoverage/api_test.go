package towercoverage

import (
	"testing"
)

const sampleAPIResponse = `Yes: Coverage ID:569863 Coverage Name:OSN.Parque de la Fuente Signal Beam:-35.2 has a Good Link;Yes: Coverage ID:53083 Coverage Name:OSN.SkyTower Signal Beam:-41.1 has a Good Link;Yes: Coverage ID:564346 Coverage Name:OSN.Santa-Cruz Signal Beam:-48.5 has a Good Link;No: Coverage ID:999 Coverage Name:OSN.Lejana Signal Beam:-60 has a Bad Link;`

func TestParsePrequalResponse(t *testing.T) {
	entries := ParsePrequalResponse(sampleAPIResponse)
	if len(entries) != 4 {
		t.Fatalf("esperaba 4 entradas, obtuvo %d", len(entries))
	}

	first := entries[0]
	if first.CoverageName != "OSN.Parque de la Fuente" {
		t.Fatalf("nombre inesperado: %q", first.CoverageName)
	}
	if first.SignalBeam != "-35.2" {
		t.Fatalf("signal inesperado: %q", first.SignalBeam)
	}
	if !isGoodLink(first) {
		t.Fatal("primera entrada debería ser Good Link")
	}
	if isGoodLink(entries[3]) {
		t.Fatal("última entrada no debería ser Good Link")
	}
}

func TestExtractXMLStringContent(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="utf-8"?><string xmlns="http://tempuri.org/">` + sampleAPIResponse + `</string>`)
	content, err := extractXMLStringContent(raw)
	if err != nil {
		t.Fatalf("error parseando XML: %v", err)
	}
	if len(ParsePrequalResponse(content)) != 4 {
		t.Fatalf("contenido XML no parseado correctamente: %q", content)
	}
}
