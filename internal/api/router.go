package api

import "net/http"

// Register monta los endpoints REST y, si swaggerEnabled, la documentación Swagger.
// jwtSecret: si está definido, /api/* exige Authorization: Bearer <jwt> firmado con
// ese secreto compartido entre las APIs de la empresa (HS256, con "exp" obligatorio).
func Register(h *Handler, jwtSecret string, swaggerEnabled bool) {
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		return withCORS(withJWT(jwtSecret, handler))
	}
	http.HandleFunc("/api/coverage", wrap(h.CoverageLight))
	http.HandleFunc("/api/coverage/full", wrap(h.CoverageFull))
	http.HandleFunc("/api/dispositivos-ap", wrap(h.DispositivosAP))
	http.HandleFunc("/api/torres", wrap(h.Torres))
	http.HandleFunc("/api/snmp/status", wrap(h.SNMPStatus))
	if swaggerEnabled {
		RegisterSwagger()
	}
}
