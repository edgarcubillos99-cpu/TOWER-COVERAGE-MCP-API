package coverage

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"tower-scraper/internal/concurrency"
	"tower-scraper/internal/db"
	"tower-scraper/internal/models"
	"tower-scraper/internal/scraper"
)

type Coord struct {
	Lat string
	Lon string
}

type consultaBloque struct {
	Lat        string                `json:"lat"`
	Lon        string                `json:"lon"`
	Resultados []models.RespuestaMCP `json:"resultados"`
	Error      string                `json:"error,omitempty"`
}

// RunConsultas ejecuta el pipeline completo de cobertura (scraper + BD + SNMP).
// Antes de consultar TowerCoverage, reutiliza resultados Redis si hay un punto
// previo dentro del radio configurado (COVERAGE_CACHE_RADIUS_M, default 25 m).
func RunConsultas(ts *scraper.TowerScraper, dbClient *db.DBClient, coords []Coord) ([]byte, error) {
	if len(coords) == 0 {
		return nil, errSinCoordenadas
	}
	store := NewResultStore(ts.CoverageRedis())
	if len(coords) == 1 {
		res, err := runForCoord(ts, dbClient, store, coords[0].Lat, coords[0].Lon)
		if err != nil {
			return nil, err
		}
		return json.MarshalIndent(res, "", "  ")
	}

	limit := concurrency.FromEnv()
	sem := make(chan struct{}, limit)
	out := make([]consultaBloque, len(coords))
	var wg sync.WaitGroup
	for i, c := range coords {
		wg.Add(1)
		go func(i int, lat, lon string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := runForCoord(ts, dbClient, store, lat, lon)
			out[i].Lat, out[i].Lon = lat, lon
			if err != nil {
				out[i].Error = err.Error()
				return
			}
			out[i].Resultados = res
		}(i, c.Lat, c.Lon)
	}
	wg.Wait()

	wrapped := struct {
		Consultas []consultaBloque `json:"consultas"`
	}{Consultas: out}
	return json.MarshalIndent(wrapped, "", "  ")
}

func runForCoord(ts *scraper.TowerScraper, dbClient *db.DBClient, store *ResultStore, lat, lon string) ([]models.RespuestaMCP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	cached, distM, err := store.FindNearestFromStrings(ctx, lat, lon)
	cancel()
	if err != nil {
		log.Printf("⚠️ Cache cobertura (full): %v — se procesará de nuevo", err)
	} else if cached != nil {
		log.Printf("Cache hit cobertura full (%.1f m de punto previo; radio %.0f m) → %d antenas",
			distM, store.RadiusM(), len(cached.Resultados))
		return cached.Resultados, nil
	}

	torres, err := ts.GetTowersData(dbClient, lat, lon)
	if err != nil {
		return nil, err
	}

	var (
		resultadosFinales []models.RespuestaMCP
		mu                sync.Mutex
		wg                sync.WaitGroup
	)

	for _, torre := range torres {
		wg.Add(1)
		go func(torre models.TowerCoverage) {
			defer wg.Done()

			log.Printf("Buscando APs en BD para la torre encontrada: %s", torre.TowerName)

			aps, err := dbClient.ObtenerAPsPorTorre(torre.TowerName)
			if err != nil {
				log.Printf("Error BD con torre %s: %v", torre.TowerName, err)
				return
			}

			if len(aps) == 0 {
				log.Printf("No hay APs configurados en DB para la torre %s", torre.TowerName)
				return
			}

			log.Printf("Se encontraron %d APs en DB para %s. Entrando a verificar...", len(aps), torre.TowerName)

			apsAnalizados, errTest := ts.TestAPCoverage(torre, aps, lat, lon)
			if errTest != nil {
				log.Printf("Fallo en la prueba de cobertura para %s: %v", torre.TowerName, errTest)
			}

			mu.Lock()
			resultadosFinales = append(resultadosFinales, apsAnalizados...)
			mu.Unlock()
		}(torre)
	}

	wg.Wait()

	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := store.Save(saveCtx, lat, lon, towersToLight(torres), resultadosFinales); err != nil {
		log.Printf("⚠️ No se pudo cachear cobertura full en Redis: %v", err)
	}
	saveCancel()

	return resultadosFinales, nil
}
