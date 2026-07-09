package towercoverage

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"tower-scraper/internal/db"
	"tower-scraper/internal/geo"
	"tower-scraper/internal/models"
)

// ResolveTowers consulta la API, cruza con la tabla torres y filtra por distancia.
func ResolveTowers(dbClient *db.DBClient, client *Client, latCliente, lonCliente string) ([]models.TowerCoverage, error) {
	latClienteFloat, err := strconv.ParseFloat(strings.TrimSpace(latCliente), 64)
	if err != nil {
		return nil, fmt.Errorf("latitud inválida: %w", err)
	}
	lonClienteFloat, err := strconv.ParseFloat(strings.TrimSpace(lonCliente), 64)
	if err != nil {
		return nil, fmt.Errorf("longitud inválida: %w", err)
	}

	raw, err := client.FetchPrequal(latCliente, lonCliente)
	if err != nil {
		return nil, err
	}

	entries := ParsePrequalResponse(raw)
	log.Printf("API TowerCoverage devolvió %d entradas para Lat: %s, Lon: %s", len(entries), latCliente, lonCliente)

	var results []models.TowerCoverage
	for _, entry := range entries {
		if !isGoodLink(entry) {
			log.Printf("❌ DESCARTADA (status no ideal): %s (%s)", entry.CoverageName, entry.LinkStatus)
			continue
		}

		torreDB, err := dbClient.ObtenerTorrePorNombre(entry.CoverageName)
		if err != nil {
			log.Printf("❌ DESCARTADA (sin torre en BD): %s — %v", entry.CoverageName, err)
			continue
		}

		latTorre, err := strconv.ParseFloat(strings.TrimSpace(torreDB.Latitud), 64)
		if err != nil {
			log.Printf("❌ DESCARTADA (latitud inválida en BD): %s — %v", entry.CoverageName, err)
			continue
		}
		lonTorre, err := strconv.ParseFloat(strings.TrimSpace(torreDB.Longitud), 64)
		if err != nil {
			log.Printf("❌ DESCARTADA (longitud inválida en BD): %s — %v", entry.CoverageName, err)
			continue
		}

		distKm := geo.CalcularDistancia(latTorre, lonTorre, latClienteFloat, lonClienteFloat)
		distMiles := geo.KmToMiles(distKm)

		if distMiles <= 0 || distMiles > maxDistanceMiles {
			log.Printf("❌ DESCARTADA (> 6 mi): %s (%.2f mi)", entry.CoverageName, distMiles)
			continue
		}

		tower := models.TowerCoverage{
			TowerName: entry.CoverageName,
			Latitude:  torreDB.Latitud,
			Longitude: torreDB.Longitud,
			Distance:  fmt.Sprintf("%.2f mi", distMiles),
			Signal:    formatSignal(entry.SignalBeam),
			Status:    entry.LinkStatus,
		}
		results = append(results, tower)
		log.Printf("✅ APROBADA: %s | Dist: %.2f mi | Status: %s", entry.CoverageName, distMiles, entry.LinkStatus)
	}

	return results, nil
}
