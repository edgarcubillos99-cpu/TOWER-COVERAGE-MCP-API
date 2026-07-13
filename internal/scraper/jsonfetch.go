package scraper

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"tower-scraper/internal/models"

	"github.com/mxschmitt/playwright-go"
)

// defaultLinkPathID es el identificador del sistema de radio en la URL
// /Dashboard/LinkPathResult/<id>. Debe coincidir con el sistema que contiene las torres
// de la cuenta (el mismo que usaba el flujo HTML). Se puede sobreescribir con TOWER_LINKPATH_ID.
const defaultLinkPathID = "31710"

// defaultClientHeight es el cHgt (altura del cliente) por defecto. Overridable con
// TOWER_CLIENT_HEIGHT.
const defaultClientHeight = "6"

// maxDistanceMiles replica el filtro del flujo HTML: solo torres a <= 6 millas.
const maxDistanceMiles = 6.0

// linkPathResponse mapea la respuesta del endpoint LinkPathResult?format=json.
// Solo se usa el arreglo "links".
type linkPathResponse struct {
	Links []linkPathEntry `json:"links"`
}

// linkPathEntry es cada elemento del arreglo "links".
type linkPathEntry struct {
	Title               string  `json:"title"`
	ImgSrc              string  `json:"imgSrc"`
	TowerLat            float64 `json:"towerLat"`
	TowerLng            float64 `json:"towerLng"`
	Elevation           float64 `json:"elevation"`
	AntennaHeight       float64 `json:"antennaHeight"`
	Alignment           float64 `json:"alignment"`
	Tilt                float64 `json:"tilt"`
	ClientHeight        float64 `json:"clientHeight"`
	RecommendationText  string  `json:"recommendationText"`
	RecommendationClass string  `json:"recommendationClass"`
	SignalText          string  `json:"signalText"`
	DistanceKm          float64 `json:"distanceKm"`
	DistanceMiles       float64 `json:"distanceMiles"`
	DistanceText        string  `json:"distanceText"`
	SuggestedHeight     string  `json:"suggestedHeight"`
	BeamHeight          string  `json:"beamHeight"`
	IsLte               bool    `json:"isLte"`
}

func linkPathID() string {
	if v := strings.TrimSpace(os.Getenv("TOWER_LINKPATH_ID")); v != "" {
		return v
	}
	return defaultLinkPathID
}

func clientHeight() string {
	if v := strings.TrimSpace(os.Getenv("TOWER_CLIENT_HEIGHT")); v != "" {
		return v
	}
	return defaultClientHeight
}

// getTowersDataViaJSON consulta el endpoint LinkPathResult con format=json reutilizando
// las cookies de la sesión (el APIRequestContext del BrowserContext las inyecta
// automáticamente en la petición). Devuelve (torres, sesiónExpirada, error).
func (s *TowerScraper) getTowersDataViaJSON(lat, lon string) ([]models.TowerCoverage, bool, error) {
	if s.context == nil {
		return nil, true, nil
	}

	url := fmt.Sprintf(
		"https://www.towercoverage.com/En-US/Dashboard/LinkPathResult/%s?format=json&Lat=%s&Lon=%s&cHgt=%s",
		linkPathID(), lat, lon, clientHeight(),
	)
	log.Printf("Consultando cobertura (JSON) para Lat=%s Lon=%s -> %s", lat, lon, url)

	// Request() comparte el estado de cookies del contexto del navegador (login).
	resp, err := s.context.Request().Get(url, playwright.APIRequestContextGetOptions{
		Timeout: playwright.Float(60000),
		Headers: map[string]string{
			"Accept":           "application/json, text/plain, */*",
			"X-Requested-With": "XMLHttpRequest",
		},
	})
	if err != nil {
		return nil, false, fmt.Errorf("error solicitando LinkPathResult JSON: %w", err)
	}
	defer resp.Dispose()

	body, err := resp.Body()
	if err != nil {
		return nil, false, fmt.Errorf("error leyendo cuerpo de LinkPathResult JSON: %w", err)
	}

	// Si la sesión expiró, el sitio responde (tras redirección) con el HTML del login.
	ct := strings.ToLower(headerValue(resp.Headers(), "content-type"))
	if !resp.Ok() || strings.Contains(ct, "text/html") || looksLikeLoginHTML(body) {
		log.Printf("[GetTowersData JSON] respuesta no-JSON (status=%d, content-type=%q); probable sesión expirada",
			resp.Status(), ct)
		return nil, true, nil
	}

	towers, err := parseLinkPathJSON(body)
	if err != nil {
		// Guardamos la respuesta cruda solo cuando falla el parseo, para depurar.
		_ = os.WriteFile("debug_linkpath.json", body, 0o644)
		return nil, false, fmt.Errorf("error parseando LinkPathResult JSON (revisa debug_linkpath.json): %w", err)
	}

	log.Printf("JSON procesado: %d torres aprobadas de %d links en LinkPathResult", len(towers), countLinks(body))
	return towers, false, nil
}

// parseLinkPathJSON toma la respuesta del endpoint y devuelve solo las torres del arreglo
// "links" que cumplen el filtro de negocio (<= 6 millas y enlace "Good").
func parseLinkPathJSON(raw []byte) ([]models.TowerCoverage, error) {
	var resp linkPathResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}

	var results []models.TowerCoverage
	for _, link := range resp.Links {
		name := cleanTowerName(link.Title)
		status := strings.TrimSpace(link.RecommendationText)

		keep := link.DistanceMiles > 0 && link.DistanceMiles <= maxDistanceMiles && statusIsGood(status)
		if keep {
			tower := models.TowerCoverage{
				TowerName: name,
				Latitude:  fmtNum(link.TowerLat),
				Longitude: fmtNum(link.TowerLng),
				Alignment: fmtNum(link.Alignment),
				Tilt:      fmtNum(link.Tilt),
				Distance:  fmt.Sprintf("%.2f mi", link.DistanceMiles),
				Signal:    strings.TrimSpace(link.SignalText),
				Status:    status,
			}
			results = append(results, tower)
			log.Printf("✅ APROBADA (JSON): %s | Align: %s, Tilt: %s, Dist: %.2f mi, Status: %s",
				tower.TowerName, tower.Alignment, tower.Tilt, link.DistanceMiles, status)
		} else {
			log.Printf("❌ DESCARTADA (JSON): %s (dist=%.2f mi, status=%q)", name, link.DistanceMiles, status)
		}
	}

	return results, nil
}

func countLinks(raw []byte) int {
	var resp linkPathResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0
	}
	return len(resp.Links)
}

func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// statusIsGood mantiene el criterio del flujo HTML original ("Good Link").
func statusIsGood(status string) bool {
	if status == "" {
		return false
	}
	return strings.Contains(strings.ToLower(status), "good")
}

func headerValue(headers map[string]string, name string) string {
	name = strings.ToLower(name)
	for k, v := range headers {
		if strings.ToLower(k) == name {
			return v
		}
	}
	return ""
}

func looksLikeLoginHTML(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "<") {
		lower := strings.ToLower(trimmed)
		return strings.Contains(lower, "login") ||
			strings.Contains(lower, `id="password"`) ||
			strings.Contains(lower, `name="username"`)
	}
	return false
}
