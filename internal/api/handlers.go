package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"tower-scraper/internal/coverage"
	"tower-scraper/internal/db"
	"tower-scraper/internal/models"
	"tower-scraper/internal/scraper"
	"tower-scraper/internal/snmp"
)

type Handler struct {
	Scraper     *scraper.TowerScraper
	DB          *db.DBClient
	ParseCoords func(raw any) ([]coverage.Coord, error)
	RateGate    coverage.RateGate // límite global RPS vía RabbitMQ (opcional)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) queryFilters(r *http.Request, allowed map[string]struct{}) map[string]string {
	out := make(map[string]string)
	for key := range allowed {
		if v := strings.TrimSpace(r.URL.Query().Get(key)); v != "" {
			out[key] = v
		}
	}
	return out
}

// CoverageFull POST /api/coverage/full — pipeline completo de cobertura (MCP get_tower_coverage).
func (h *Handler) CoverageFull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "método no permitido; usa POST")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "no se pudo leer el cuerpo")
		return
	}

	dec := json.NewDecoder(bytes.NewReader(bodyBytes))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("JSON inválido: %v", err))
		return
	}

	coords, err := h.ParseCoords(raw)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	payload, err := coverage.RunConsultas(h.Scraper, h.DB, coords, h.RateGate, true)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("fallo consulta cobertura: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

// CoverageLight POST /api/coverage — torres aprobadas vía GetSiteList + LinkPathAPI
// (imagen path + datos tipo resumen visual; cruza BD torres, sin SNMP).
// Si existe una cobertura full previa dentro del radio de cache (default 25 m),
// reutiliza las torres guardadas y evita llamar a TowerCoverage.
func (h *Handler) CoverageLight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "método no permitido; usa POST")
		return
	}

	var reqBody struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		h.writeError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	store := coverage.NewResultStore(h.Scraper.CoverageRedis())
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	cached, distM, err := store.FindNearestFromStrings(ctx, reqBody.Lat, reqBody.Lon)
	cancel()
	if err != nil {
		log.Printf("⚠️ Cache cobertura (light): %v — se consultará la API", err)
	} else if cached != nil {
		log.Printf("Cache hit cobertura light (%.1f m de punto previo; radio %.0f m) → %d torres",
			distM, store.RadiusM(), len(cached.Towers))
		out := cached.Towers
		if out == nil {
			out = []models.CoverageLightItem{}
		}
		h.writeJSON(w, http.StatusOK, out)
		return
	}

	rateCtx, rateCancel := context.WithTimeout(r.Context(), 2*time.Minute)
	if err := h.acquireRate(rateCtx); err != nil {
		rateCancel()
		h.writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	rateCancel()
	log.Printf("Cupo TowerCoverage adquirido (light) para Lat: %s, Lon: %s", reqBody.Lat, reqBody.Lon)

	torres, err := h.Scraper.GetTowersData(h.DB, reqBody.Lat, reqBody.Lon)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("error obteniendo datos: %v", err))
		return
	}

	out := make([]models.CoverageLightItem, 0, len(torres))
	for _, t := range torres {
		out = append(out, t.ToCoverageLight())
	}
	h.writeJSON(w, http.StatusOK, out)
}

func (h *Handler) acquireRate(ctx context.Context) error {
	if h == nil || h.RateGate == nil {
		return nil
	}
	return h.RateGate.Acquire(ctx)
}

var dispositivoAPQueryKeys = map[string]struct{}{
	"id": {}, "disp_id": {}, "torre_nombre": {}, "ap_name": {},
	"tipo": {}, "azimut": {}, "tilt": {}, "altura": {}, "ip_address": {},
}

// DispositivosAP GET /api/dispositivos-ap — consulta dispositivos_ap por cualquier columna.
func (h *Handler) DispositivosAP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "método no permitido; usa GET")
		return
	}

	rows, err := h.DB.ListDispositivosAP(h.queryFilters(r, dispositivoAPQueryKeys))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(rows) == 0 {
		rows = []models.DispositivoAP{}
	}
	h.writeJSON(w, http.StatusOK, rows)
}

var torreQueryKeys = map[string]struct{}{
	"id": {}, "nombre": {}, "latitud": {}, "longitud": {},
}

// Torres GET /api/torres — consulta tabla torres por id, nombre, latitud y/o longitud.
func (h *Handler) Torres(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "método no permitido; usa GET")
		return
	}

	rows, err := h.DB.ListTorres(h.queryFilters(r, torreQueryKeys))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(rows) == 0 {
		rows = []models.TorreDB{}
	}
	h.writeJSON(w, http.StatusOK, rows)
}

// SNMPStatus GET /api/snmp/status — consulta SNMP por ip_address o ap_name (+ torre_nombre opcional).
func (h *Handler) SNMPStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "método no permitido; usa GET")
		return
	}

	ip := strings.TrimSpace(r.URL.Query().Get("ip_address"))
	apName := strings.TrimSpace(r.URL.Query().Get("ap_name"))
	torreNombre := strings.TrimSpace(r.URL.Query().Get("torre_nombre"))
	tipoOverride := strings.TrimSpace(r.URL.Query().Get("tipo"))

	if ip == "" && apName == "" {
		h.writeError(w, http.StatusBadRequest, "indica ip_address o ap_name")
		return
	}

	dev, err := h.DB.ResolveDispositivoAP(ip, apName, torreNombre)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "indica ip_address") {
			h.writeError(w, http.StatusBadRequest, msg)
			return
		}
		if strings.Contains(msg, "coincide con") {
			h.writeError(w, http.StatusConflict, msg)
			return
		}
		h.writeError(w, http.StatusNotFound, msg)
		return
	}

	if tipoOverride != "" {
		dev.Tipo = tipoOverride
	}

	ipAddr := strings.TrimSpace(dev.IPAddress)
	if ipAddr == "" {
		h.writeError(w, http.StatusBadRequest, "el dispositivo no tiene ip_address en BD")
		return
	}
	if strings.TrimSpace(dev.Tipo) == "" {
		h.writeError(w, http.StatusBadRequest, "el dispositivo no tiene tipo en BD; indica tipo en la query")
		return
	}

	st, err := snmp.CheckSaturation(models.AccessPoint{
		TowerName: dev.TorreNombre,
		APName:    dev.APName,
		Tipo:      dev.Tipo,
		IPAddress: ipAddr,
	})
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("fallo SNMP: %v", err))
		return
	}

	h.writeJSON(w, http.StatusOK, snmpStatusResponse(dev, st))
}

func snmpStatusResponse(dev models.DispositivoAP, st models.APStatus) models.SNMPStatusResponse {
	out := models.SNMPStatusResponse{
		APName:             dev.APName,
		TorreNombre:        dev.TorreNombre,
		Tipo:               dev.Tipo,
		IPAddress:          strings.TrimSpace(dev.IPAddress),
		ClientesConectados: st.Clients,
	}
	msg := strings.TrimSpace(st.Message)
	if cap := strings.TrimSpace(st.EstadoCapacidad); cap != "" {
		out.EstadoCapacidad = cap
	} else if msg != "" {
		out.EstadoCapacidad = msg
	}
	if msg == "Saturado" || msg == "Con espacio" {
		out.EstaSaturado = st.IsSaturated
	}
	return out
}
