package geo

import "strings"

// ObtenerApertura devuelve los grados de apertura (beamwidth) según tipo y nombre del AP.
// OMNI en tipo o ap_name → 360°; Wave/Wabe → 30°; resto → 90° por defecto.
func ObtenerApertura(tipo, apName string) float64 {
	tipoNorm := strings.ToLower(strings.TrimSpace(tipo))
	apNorm := strings.ToLower(strings.TrimSpace(apName))
	if strings.Contains(tipoNorm, "omni") || strings.Contains(apNorm, "omni") {
		return 360.0
	}
	if strings.Contains(tipoNorm, "wave") || strings.Contains(tipoNorm, "wabe") {
		return 30.0
	}
	return 90.0
}
