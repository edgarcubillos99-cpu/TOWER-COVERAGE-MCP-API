package towercoverage

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"tower-scraper/internal/concurrency"
	"tower-scraper/internal/db"
	"tower-scraper/internal/geo"
	"tower-scraper/internal/models"
)

// ResolveTowers obtiene sitios vía GetSiteList, filtra por distancia (<= 6 mi),
// verifica que existan en la tabla torres (por nombre, sin prefijo OSN.),
// evalúa cada candidato con LinkPathAPI y conserva solo enlaces posibles.
func ResolveTowers(dbClient *db.DBClient, client *Client, latCliente, lonCliente string) ([]models.TowerCoverage, error) {
	latClienteFloat, err := strconv.ParseFloat(strings.TrimSpace(latCliente), 64)
	if err != nil {
		return nil, fmt.Errorf("latitud inválida: %w", err)
	}
	lonClienteFloat, err := strconv.ParseFloat(strings.TrimSpace(lonCliente), 64)
	if err != nil {
		return nil, fmt.Errorf("longitud inválida: %w", err)
	}

	sites, err := client.GetSites(context.Background())
	if err != nil {
		return nil, err
	}
	log.Printf("Listado de sitios disponible: %d", len(sites))

	nameSet, err := dbClient.NombresTorresSet()
	if err != nil {
		return nil, fmt.Errorf("cargando nombres de torres: %w", err)
	}

	type nearbySite struct {
		site      Site
		distMiles float64
	}

	var nearby []nearbySite
	for _, site := range sites {
		name := strings.TrimSpace(site.Name)
		if name == "" {
			continue
		}
		if strings.TrimSpace(site.Error) != "" || strings.TrimSpace(site.Errors) != "" {
			log.Printf("❌ DESCARTADA (error en sitio): %s — %s%s", name, site.Error, site.Errors)
			continue
		}

		distKm := geo.CalcularDistancia(site.Latitude, site.Longitude, latClienteFloat, lonClienteFloat)
		distMiles := geo.KmToMiles(distKm)
		if distMiles <= 0 || distMiles > maxDistanceMiles {
			log.Printf("❌ DESCARTADA (> 6 mi): %s (%.2f mi)", name, distMiles)
			continue
		}

		if !db.TorreEnSet(nameSet, name) {
			log.Printf("❌ DESCARTADA (no está en BD torres): %s", name)
			continue
		}

		nearby = append(nearby, nearbySite{site: site, distMiles: distMiles})
		log.Printf("📍 Candidata (<= 6 mi + en BD): %s (%.2f mi) — evaluando LinkPathAPI", name, distMiles)
	}

	if len(nearby) == 0 {
		log.Printf("Ninguna torre candidata (≤ %.0f mi y en BD) para Lat: %s, Lon: %s", maxDistanceMiles, latCliente, lonCliente)
		return nil, nil
	}

	type evalResult struct {
		tower models.TowerCoverage
		ok    bool
	}
	resultsCh := make([]evalResult, len(nearby))

	limit := concurrency.FromEnv()
	var wg sync.WaitGroup
	sem := make(chan struct{}, limit)

	for i, n := range nearby {
		wg.Add(1)
		go func(i int, n nearbySite) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			site := n.site
			height := formatHeight(site.Height)
			if site.Height <= 0 {
				height = "15"
			}

			link, err := client.FetchLinkPath(
				site.ID,
				formatCoord(site.Latitude),
				formatCoord(site.Longitude),
				height,
				strings.TrimSpace(latCliente),
				strings.TrimSpace(lonCliente),
			)
			if err != nil {
				log.Printf("❌ DESCARTADA (LinkPathAPI error): %s — %v", site.Name, err)
				return
			}
			if !isPossibleLink(link) {
				log.Printf("❌ DESCARTADA (link no posible): %s | margin=%s dBm=%s err=%q",
					site.Name, link.SignalMargin, link.SignalInDBm, strings.TrimSpace(link.Error))
				return
			}

			latStr := formatCoord(site.Latitude)
			lonStr := formatCoord(site.Longitude)
			if v := strings.TrimSpace(link.LeftsiteLatitude); v != "" {
				latStr = v
			}
			if v := strings.TrimSpace(link.LeftsiteLongitude); v != "" {
				lonStr = v
			}

			towerHeight := strings.TrimSpace(link.LeftsiteAntennaHeight)
			if towerHeight == "" {
				towerHeight = height
			}
			clientHeight := strings.TrimSpace(link.RightsiteAntennaHeight)
			if clientHeight == "" {
				clientHeight = defaultSite2AntennaHeight
			}
			status := "Good Link"

			resultsCh[i] = evalResult{
				ok: true,
				tower: models.TowerCoverage{
					TowerName:       site.Name,
					Group:           strings.TrimSpace(site.Group),
					Latitude:        latStr,
					Longitude:       lonStr,
					Alignment:       withUnit(link.RightsiteLinkAzimuth, "°"),
					Tilt:            withUnit(link.LeftsiteLinkTilt, "°"),
					Distance:        formatDistanceMi(n.distMiles),
					DistanceKm:      formatDistanceKm(n.distMiles),
					Signal:          formatSignalRSSI(link.SignalInDBm),
					Status:          status,
					Elevation:       withUnit(link.LeftsiteGroundElevation, "m"),
					TowerHeight:     withUnit(towerHeight, "m"),
					ClientHeight:    clientHeight,
					SuggestedHeight: withUnit(link.MinimumAntennaHeight, "m"),
					PathImage:       strings.TrimSpace(link.PathImage),
				},
			}
			log.Printf("✅ APROBADA: %s | Dist: %.2f mi | Signal: %s | Margin: %s",
				site.Name, n.distMiles, formatSignal(link.SignalInDBm), link.SignalMargin)
		}(i, n)
	}
	wg.Wait()

	var results []models.TowerCoverage
	for _, r := range resultsCh {
		if r.ok {
			results = append(results, r.tower)
		}
	}
	return results, nil
}
