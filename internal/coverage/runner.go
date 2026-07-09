package coverage

import (
	"encoding/json"
	"log"
	"sync"

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
func RunConsultas(ts *scraper.TowerScraper, dbClient *db.DBClient, coords []Coord) ([]byte, error) {
	if len(coords) == 0 {
		return nil, errSinCoordenadas
	}
	if len(coords) == 1 {
		res, err := runForCoord(ts, dbClient, coords[0].Lat, coords[0].Lon)
		if err != nil {
			return nil, err
		}
		return json.MarshalIndent(res, "", "  ")
	}

	out := make([]consultaBloque, len(coords))
	var wg sync.WaitGroup
	for i, c := range coords {
		wg.Add(1)
		go func(i int, lat, lon string) {
			defer wg.Done()
			res, err := runForCoord(ts, dbClient, lat, lon)
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

func runForCoord(ts *scraper.TowerScraper, dbClient *db.DBClient, lat, lon string) ([]models.RespuestaMCP, error) {
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
	return resultadosFinales, nil
}
