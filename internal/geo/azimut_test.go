package geo

import "testing"

func TestParsearAzimut(t *testing.T) {
	tests := []struct {
		in     string
		want   float64
		wantOK bool
	}{
		{"76°E", 76, true},
		{"34°N", 34, true},
		{"321°NW", 321, true},
		{"248", 248, true},
		{"  230.5  ", 230.5, true},
		{"", 0, false},
		{"   ", 0, false},
		{"sin datos", 0, false},
	}
	for _, tc := range tests {
		got, ok := ParsearAzimut(tc.in)
		if ok != tc.wantOK {
			t.Fatalf("ParsearAzimut(%q) ok=%v, want %v", tc.in, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Fatalf("ParsearAzimut(%q)=%v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestObtenerApertura(t *testing.T) {
	if got := ObtenerApertura("Ubiquiti Wave AP", ""); got != 30 {
		t.Fatalf("Wave: got %v", got)
	}
	if got := ObtenerApertura("WABE-60", ""); got != 30 {
		t.Fatalf("Wabe: got %v", got)
	}
	if got := ObtenerApertura("AirMax AC", ""); got != 90 {
		t.Fatalf("default: got %v", got)
	}
	if got := ObtenerApertura("Rocket AC OMNI", ""); got != 360 {
		t.Fatalf("OMNI en tipo: got %v", got)
	}
	if got := ObtenerApertura("ePMP3000", "OSNAP6-D ePMP3000 (OMNI)"); got != 360 {
		t.Fatalf("OMNI en ap_name: got %v", got)
	}
	if got := ObtenerApertura("ePMP2000 OMNI", "OSNAP41-A"); got != 360 {
		t.Fatalf("OMNI en tipo y ap_name: got %v", got)
	}
}

func TestEstaEnCoberturaOmni(t *testing.T) {
	if !EstaEnCobertura(0, 180, 360) {
		t.Fatal("beamwidth 360 debe cubrir cualquier bearing")
	}
}
