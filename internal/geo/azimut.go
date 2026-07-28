package geo

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var rePrimerNumeroAzimut = regexp.MustCompile(`(\d+(?:\.\d+)?)`)

// ParsearAzimut extrae el azimut numérico en grados desde formatos de BD como
// "76°E", "34°N", "321°NW", "248" o cadenas vacías/NULL (devuelve ok=false).
func ParsearAzimut(raw string) (grados float64, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	m := rePrimerNumeroAzimut.FindStringSubmatch(raw)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	v = math.Mod(v, 360)
	if v < 0 {
		v += 360
	}
	return v, true
}
