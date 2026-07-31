package coverage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"tower-scraper/internal/models"
)

const (
	coverageGeoKey     = "towercoverage:coverage:geo"
	coverageDataPrefix = "towercoverage:coverage:data:"
	// defaultCacheRadiusM: radio (metros) para reutilizar una cobertura previa.
	defaultCacheRadiusM = 25.0
)

// CachedCoverage es el payload guardado tras una cobertura full.
type CachedCoverage struct {
	Lat        float64                    `json:"lat"`
	Lon        float64                    `json:"lon"`
	Towers     []models.CoverageLightItem `json:"towers"`
	Resultados []models.RespuestaMCP      `json:"resultados"`
	CachedAt   time.Time                  `json:"cached_at"`
}

// ResultStore busca y guarda coberturas en Redis por proximidad geográfica.
type ResultStore struct {
	rdb      *redis.Client
	radiusM  float64
	geoKey   string
	dataPref string
}

// NewResultStore crea un store de resultados de cobertura. Si rdb es nil, las
// operaciones son no-op (miss / no guarda).
func NewResultStore(rdb *redis.Client) *ResultStore {
	return &ResultStore{
		rdb:      rdb,
		radiusM:  radiusFromEnv(),
		geoKey:   coverageGeoKey,
		dataPref: coverageDataPrefix,
	}
}

func radiusFromEnv() float64 {
	v := strings.TrimSpace(os.Getenv("COVERAGE_CACHE_RADIUS_M"))
	if v == "" {
		return defaultCacheRadiusM
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n <= 0 {
		return defaultCacheRadiusM
	}
	return n
}

// RadiusM devuelve el radio de reutilización configurado (metros).
func (s *ResultStore) RadiusM() float64 {
	if s == nil || s.radiusM <= 0 {
		return defaultCacheRadiusM
	}
	return s.radiusM
}

// FindNearest busca la cobertura más cercana dentro del radio configurado.
// Devuelve (nil, 0, nil) si no hay hit o Redis no está disponible.
func (s *ResultStore) FindNearest(ctx context.Context, lat, lon float64) (*CachedCoverage, float64, error) {
	if s == nil || s.rdb == nil {
		return nil, 0, nil
	}

	locs, err := s.rdb.GeoRadius(ctx, s.geoKey, lon, lat, &redis.GeoRadiusQuery{
		Radius:   s.RadiusM(),
		Unit:     "m",
		WithDist: true,
		Count:    8, // varias candidatas; elegimos la de menor distancia
		Sort:     "ASC",
	}).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("GeoRadius cobertura: %w", err)
	}
	if len(locs) == 0 {
		return nil, 0, nil
	}

	// ASC ya ordena por distancia; tomamos la primera con payload válido.
	for _, loc := range locs {
		raw, err := s.rdb.Get(ctx, s.dataPref+loc.Name).Bytes()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, 0, fmt.Errorf("leyendo cobertura cacheada %s: %w", loc.Name, err)
		}
		var cached CachedCoverage
		if err := json.Unmarshal(raw, &cached); err != nil {
			log.Printf("⚠️ Cobertura cacheada corrupta (%s): %v — se ignora", loc.Name, err)
			continue
		}
		return &cached, loc.Dist, nil
	}
	return nil, 0, nil
}

// FindNearestFromStrings parsea lat/lon string y busca en el cache.
func (s *ResultStore) FindNearestFromStrings(ctx context.Context, latStr, lonStr string) (*CachedCoverage, float64, error) {
	lat, lon, err := parseLatLon(latStr, lonStr)
	if err != nil {
		return nil, 0, err
	}
	return s.FindNearest(ctx, lat, lon)
}

// Save persiste torres (formato light) y resultados full en el índice geo.
func (s *ResultStore) Save(ctx context.Context, latStr, lonStr string, towers []models.CoverageLightItem, resultados []models.RespuestaMCP) error {
	if s == nil || s.rdb == nil {
		return nil
	}
	lat, lon, err := parseLatLon(latStr, lonStr)
	if err != nil {
		return err
	}
	if towers == nil {
		towers = []models.CoverageLightItem{}
	}
	if resultados == nil {
		resultados = []models.RespuestaMCP{}
	}

	payload := CachedCoverage{
		Lat:        lat,
		Lon:        lon,
		Towers:     towers,
		Resultados: resultados,
		CachedAt:   time.Now().UTC(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("serializando cobertura: %w", err)
	}

	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	pipe := s.rdb.TxPipeline()
	pipe.GeoAdd(ctx, s.geoKey, &redis.GeoLocation{
		Name:      id,
		Longitude: lon,
		Latitude:  lat,
	})
	pipe.Set(ctx, s.dataPref+id, raw, 0)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("guardando cobertura en Redis: %w", err)
	}
	log.Printf("Cobertura cacheada en Redis (id=%s, lat=%.6f, lon=%.6f, torres=%d, antenas=%d, radio=%.0fm)",
		id, lat, lon, len(towers), len(resultados), s.RadiusM())
	return nil
}

func parseLatLon(latStr, lonStr string) (float64, float64, error) {
	lat, err := strconv.ParseFloat(strings.TrimSpace(latStr), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("lat inválida %q: %w", latStr, err)
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(lonStr), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("lon inválida %q: %w", lonStr, err)
	}
	return lat, lon, nil
}

func towersToLight(torres []models.TowerCoverage) []models.CoverageLightItem {
	out := make([]models.CoverageLightItem, 0, len(torres))
	for _, t := range torres {
		out = append(out, t.ToCoverageLight())
	}
	return out
}

// enrichResultadosPathImage rellena path_image y el resumen de sitio (group/tower/
// client/performance) en cada antena desde las torres cacheadas. Los campos van
// con json:"-" en RespuestaMCP y no persisten en Redis; en cache hit hay que
// rehidratarlos (mismo valor para todas las antenas de un sitio).
func enrichResultadosPathImage(resultados []models.RespuestaMCP, towers []models.CoverageLightItem) []models.RespuestaMCP {
	if len(resultados) == 0 || len(towers) == 0 {
		return resultados
	}
	byName := make(map[string]models.CoverageLightItem, len(towers))
	for _, t := range towers {
		byName[t.TowerName] = t
	}
	if len(byName) == 0 {
		return resultados
	}
	for i := range resultados {
		t, ok := byName[resultados[i].NombreTorre]
		if !ok {
			continue
		}
		if strings.TrimSpace(resultados[i].PathImage) == "" {
			resultados[i].PathImage = t.PathImage
		}
		if strings.TrimSpace(resultados[i].Group) == "" &&
			strings.TrimSpace(resultados[i].Client.SiteTilt) == "" &&
			strings.TrimSpace(resultados[i].Performance.Status) == "" {
			resultados[i].Group, resultados[i].Tower, resultados[i].Client, resultados[i].Performance =
				models.SiteFieldsFromLight(t)
		}
	}
	return resultados
}
