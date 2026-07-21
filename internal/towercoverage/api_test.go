package towercoverage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestIsPossibleLink(t *testing.T) {
	cases := []struct {
		name   string
		result *LinkPathResult
		want   bool
	}{
		{
			name:   "nil",
			result: nil,
			want:   false,
		},
		{
			name: "con error API",
			result: &LinkPathResult{
				SignalMargin: "10",
				Error:        "Error Code:API00000123; Message:The Account ID or key provided is not valid.",
			},
			want: false,
		},
		{
			name: "margen negativo",
			result: &LinkPathResult{
				SignalMargin: "-49.2",
				SignalInDBm:  "-121.2",
			},
			want: false,
		},
		{
			name: "margen cero",
			result: &LinkPathResult{
				SignalMargin: "0",
				SignalInDBm:  "-72",
			},
			want: true,
		},
		{
			name: "margen positivo",
			result: &LinkPathResult{
				SignalMargin: "22.5",
				SignalInDBm:  "-49.5",
			},
			want: true,
		},
		{
			name: "margen inválido",
			result: &LinkPathResult{
				SignalMargin: "",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPossibleLink(tc.result); got != tc.want {
				t.Fatalf("isPossibleLink() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLinkPathResultUnmarshalCasing(t *testing.T) {
	rawLower := []byte(`{"signalmargin":"14.7","signalindBm":"-57.3","error":"","leftsite_linkazimuth":"96.1","leftsite_linktilt":"-0.2"}`)
	var lower LinkPathResult
	if err := json.Unmarshal(rawLower, &lower); err != nil {
		t.Fatalf("unmarshal lower: %v", err)
	}
	if lower.SignalMargin != "14.7" || lower.SignalInDBm != "-57.3" {
		t.Fatalf("lower casing mal parseado: %+v", lower)
	}

	rawDoc := []byte(`{"Signalmargin":"14.7","SignalindBm":"-57.3","Error":"","Leftsite_Name":"Tower North"}`)
	var doc LinkPathResult
	if err := json.Unmarshal(rawDoc, &doc); err != nil {
		t.Fatalf("unmarshal doc casing: %v", err)
	}
	if doc.SignalMargin != "14.7" || doc.SignalInDBm != "-57.3" || doc.LeftsiteName != "Tower North" {
		t.Fatalf("doc casing mal parseado: %+v", doc)
	}
	if !isPossibleLink(&doc) {
		t.Fatal("respuesta doc debería ser link posible")
	}
}

func TestFormatSignal(t *testing.T) {
	if got := formatSignal("-49.5"); got != "-49.5 dBm" {
		t.Fatalf("formatSignal inesperado: %q", got)
	}
	if got := formatSignal(""); got != "" {
		t.Fatalf("formatSignal vacío inesperado: %q", got)
	}
}

func TestFormatSignalRSSI(t *testing.T) {
	if got := formatSignalRSSI("-49.4"); got != "-49.4:RSSI" {
		t.Fatalf("formatSignalRSSI inesperado: %q", got)
	}
}

func TestWithUnit(t *testing.T) {
	if got := withUnit("18", "m"); got != "18m" {
		t.Fatalf("withUnit: %q", got)
	}
	if got := withUnit("18m", "m"); got != "18m" {
		t.Fatalf("withUnit idempotente: %q", got)
	}
	if got := withUnit("64.59", "°"); got != "64.59°" {
		t.Fatalf("withUnit grados: %q", got)
	}
}

func TestFormatCoord(t *testing.T) {
	if got := formatCoord(18.191159); got != "18.191159" {
		t.Fatalf("formatCoord inesperado: %q", got)
	}
}

func TestDurationUntilNextSunday(t *testing.T) {
	// Miércoles 15:00 → próximo domingo 03:00 (4 días menos 12h = ~3.5d)
	wed := time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local) // miércoles
	if wed.Weekday() != time.Wednesday {
		t.Fatalf("fixture weekday: %v", wed.Weekday())
	}
	d := durationUntilNextSunday(wed, 3, 0)
	next := wed.Add(d)
	if next.Weekday() != time.Sunday || next.Hour() != 3 {
		t.Fatalf("esperado domingo 03:00, obtuvo %v", next)
	}

	// Domingo 04:00 → siguiente domingo 03:00
	sun := time.Date(2026, 7, 19, 4, 0, 0, 0, time.Local)
	if sun.Weekday() != time.Sunday {
		t.Fatalf("fixture weekday: %v", sun.Weekday())
	}
	d2 := durationUntilNextSunday(sun, 3, 0)
	next2 := sun.Add(d2)
	if next2.Weekday() != time.Sunday || next2.Hour() != 3 || !next2.After(sun) {
		t.Fatalf("esperado próximo domingo 03:00, obtuvo %v", next2)
	}
}
