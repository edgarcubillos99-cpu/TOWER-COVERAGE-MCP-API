package apsync

import "testing"

func TestParseCoverageNameExamples(t *testing.T) {
	cases := []struct {
		name  string
		sitio string
		ap    string
		tipo  string
	}{
		{"OSN-COLLORES TOWER-OSNAP16-A ePMP3000", "COLLORES TOWER", "OSNAP16-A", "ePMP3000"},
		{"OSN-MARIANA-OSNAP7-K ePMP3000", "MARIANA", "OSNAP7-K", "ePMP3000"},
		{"OSN-LECHUZA-OSNAP67-F Rocket AC", "LECHUZA", "OSNAP67-F", "Rocket AC"},
		{"OSN-VESTA-VESTSEC-6 ePMP3000", "VESTA", "VESTSEC-6", "ePMP3000"},
		{"OSN-CULEBRA NOC-CAP2 Rocket 5AC Lite", "CULEBRA NOC", "CAP2", "Rocket 5AC Lite"},
		{"OSN-CORCOBADA-CORSEC-1 Rocket 5AC lite", "CORCOBADA", "CORSEC-1", "Rocket 5AC lite"},
		{"OSN-LA-TABLA-OSNAP48-A ePMP3000", "LA-TABLA", "OSNAP48-A", "ePMP3000"},
		{"OSN-FOO-ARECIBO-1 ePMP3000", "FOO", "ARECIBO-1", "ePMP3000"},
		{"OSN-BAR-FLINK2-A Rocket AC", "BAR", "FLINK2-A", "Rocket AC"},
		{"OSN-BAZ-4NET-196-A ePMP3000", "BAZ", "4NET-196-A", "ePMP3000"},
		{"OSN-LA SANTA-OSANP12-A ePMP3000", "LA SANTA", "OSANP12-A", "ePMP3000"},
		{"osn-mariana-osnap7-k ePMP3000", "mariana", "osnap7-k", "ePMP3000"},
	}
	for _, tc := range cases {
		got, ok := ParseCoverageName(tc.name)
		if !ok {
			t.Fatalf("ParseCoverageName(%q) = false", tc.name)
		}
		if got.Sitio != tc.sitio || got.AP != tc.ap || got.Tipo != tc.tipo {
			t.Fatalf("%q → %+v, want sitio=%q ap=%q tipo=%q", tc.name, got, tc.sitio, tc.ap, tc.tipo)
		}
	}
}

func TestParseCoverageNameIgnoresNonOSN(t *testing.T) {
	for _, name := range []string{
		"",
		"5GHz Coverage",
		"OSN.COLLORES TOWER-OSNAP16-A ePMP3000",
		"COLLORES TOWER-OSNAP16-A ePMP3000",
	} {
		if _, ok := ParseCoverageName(name); ok {
			t.Fatalf("esperaba ignorar %q", name)
		}
	}
}

func TestTipoConOMNI(t *testing.T) {
	if got := TipoConOMNI("ePMP3000", "360"); got != "ePMP3000 OMNI" {
		t.Fatalf("360: %q", got)
	}
	if got := TipoConOMNI("ePMP3000", "0"); got != "ePMP3000 OMNI" {
		t.Fatalf("0: %q", got)
	}
	if got := TipoConOMNI("Rocket AC OMNI", "360"); got != "Rocket AC OMNI" {
		t.Fatalf("ya omni: %q", got)
	}
	if got := TipoConOMNI("ePMP3000", "90"); got != "ePMP3000" {
		t.Fatalf("90: %q", got)
	}
}

func TestTorreNombreDesdeSite(t *testing.T) {
	if got := TorreNombreDesdeSite("OSN.COLLORES TOWER", "COLLORES TOWER"); got != "COLLORES TOWER" {
		t.Fatalf("site: %q", got)
	}
	if got := TorreNombreDesdeSite("", "LA-TABLA"); got != "LA-TABLA" {
		t.Fatalf("fallback: %q", got)
	}
}
