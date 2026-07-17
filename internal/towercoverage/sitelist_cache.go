package towercoverage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	siteListRedisKey   = "towercoverage:sitelist"
	siteListVersionKey = "towercoverage:sitelist:version"
	// siteListCacheVersion: subir cuando cambie el contenido cacheado (p. ej. enriquecimiento).
	// v2 = height tomado de GetCoverageList.antennaheight.
	siteListCacheVersion = "2"
)

// SiteListStore cachea el resultado de GetSiteList en Redis.
type SiteListStore struct {
	rdb        *redis.Client
	key        string
	versionKey string
}

func NewSiteListStore(rdb *redis.Client) *SiteListStore {
	return &SiteListStore{
		rdb:        rdb,
		key:        siteListRedisKey,
		versionKey: siteListVersionKey,
	}
}

func (s *SiteListStore) Get(ctx context.Context) ([]Site, error) {
	if s == nil || s.rdb == nil {
		return nil, fmt.Errorf("store Redis no configurado")
	}
	raw, err := s.rdb.Get(ctx, s.key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leyendo sitelist de Redis: %w", err)
	}
	var sites []Site
	if err := json.Unmarshal(raw, &sites); err != nil {
		return nil, fmt.Errorf("parseando sitelist de Redis: %w", err)
	}
	return sites, nil
}

// IsCurrent indica si el cache tiene la versión esperada por el código.
func (s *SiteListStore) IsCurrent(ctx context.Context) (bool, error) {
	if s == nil || s.rdb == nil {
		return false, fmt.Errorf("store Redis no configurado")
	}
	v, err := s.rdb.Get(ctx, s.versionKey).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("leyendo versión sitelist: %w", err)
	}
	return v == siteListCacheVersion, nil
}

func (s *SiteListStore) Set(ctx context.Context, sites []Site) error {
	if s == nil || s.rdb == nil {
		return fmt.Errorf("store Redis no configurado")
	}
	raw, err := json.Marshal(sites)
	if err != nil {
		return fmt.Errorf("serializando sitelist: %w", err)
	}
	// Sin TTL: la caducidad la controla el refresco semanal (domingos).
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, s.key, raw, 0)
	pipe.Set(ctx, s.versionKey, siteListCacheVersion, 0)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("guardando sitelist en Redis: %w", err)
	}
	return nil
}

// fetchSitesEnriched obtiene GetSiteList, aplica antennaheight de GetCoverageList
// y devuelve el listado listo para cachear o usar.
func (c *Client) fetchSitesEnriched() ([]Site, error) {
	sites, err := c.FetchSiteList()
	if err != nil {
		return nil, err
	}
	coverages, err := c.FetchCoverageList()
	if err != nil {
		return nil, fmt.Errorf("GetCoverageList: %w", err)
	}
	n := applyCoverageAntennaHeights(sites, coverages)
	log.Printf("GetCoverageList: %d coberturas; height actualizado en %d sitios", len(coverages), n)
	return sites, nil
}

func (c *Client) cacheHit(ctx context.Context) ([]Site, bool) {
	if c.SiteStore == nil {
		return nil, false
	}
	current, err := c.SiteStore.IsCurrent(ctx)
	if err != nil {
		log.Printf("⚠️ Redis sitelist versión: %v — se consultará la API", err)
		return nil, false
	}
	if !current {
		return nil, false
	}
	sites, err := c.SiteStore.Get(ctx)
	if err != nil {
		log.Printf("⚠️ Redis sitelist: %v — se consultará la API", err)
		return nil, false
	}
	if len(sites) == 0 {
		return nil, false
	}
	return sites, true
}

// GetSites devuelve el listado desde Redis si existe y está actualizado;
// si no, llama a la API (sites + coverages) y lo cachea.
func (c *Client) GetSites(ctx context.Context) ([]Site, error) {
	if sites, ok := c.cacheHit(ctx); ok {
		log.Printf("GetSiteList desde Redis (%d sitios)", len(sites))
		return sites, nil
	}

	sites, err := c.fetchSitesEnriched()
	if err != nil {
		return nil, err
	}
	if c.SiteStore != nil {
		if err := c.SiteStore.Set(ctx, sites); err != nil {
			log.Printf("⚠️ No se pudo cachear sitelist en Redis: %v", err)
		} else {
			log.Printf("GetSiteList cacheado en Redis (%d sitios, v%s)", len(sites), siteListCacheVersion)
		}
	}
	return sites, nil
}

// RefreshSiteList fuerza GetSiteList + GetCoverageList a la API y actualiza Redis.
func (c *Client) RefreshSiteList(ctx context.Context) error {
	sites, err := c.fetchSitesEnriched()
	if err != nil {
		return err
	}
	if c.SiteStore == nil {
		return fmt.Errorf("store Redis no configurado")
	}
	if err := c.SiteStore.Set(ctx, sites); err != nil {
		return err
	}
	log.Printf("GetSiteList refrescado en Redis (%d sitios, v%s)", len(sites), siteListCacheVersion)
	return nil
}

// EnsureSiteListCache rellena Redis si está vacío o con versión antigua (arranque).
func (c *Client) EnsureSiteListCache(ctx context.Context) error {
	if c.SiteStore == nil {
		return nil
	}
	if sites, ok := c.cacheHit(ctx); ok {
		log.Printf("Redis ya tiene sitelist actualizado (%d sitios, v%s)", len(sites), siteListCacheVersion)
		return nil
	}
	current, err := c.SiteStore.IsCurrent(ctx)
	if err != nil {
		return err
	}
	if !current {
		log.Printf("Redis sitelist desactualizado o sin versión (esperada v%s); refrescando con GetSiteList + GetCoverageList...", siteListCacheVersion)
	} else {
		log.Println("Redis sin sitelist; obteniendo GetSiteList + GetCoverageList de la API...")
	}
	return c.RefreshSiteList(ctx)
}

// StartSundaySiteListRefresh lanza un goroutine que refresca GetSiteList cada domingo a las 03:00 local.
func (c *Client) StartSundaySiteListRefresh(ctx context.Context) {
	if c.SiteStore == nil {
		return
	}
	go func() {
		for {
			wait := durationUntilNextSunday(time.Now(), 3, 0)
			log.Printf("Próximo refresco GetSiteList (domingo 03:00) en %s", wait.Round(time.Minute))
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				refreshCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				if err := c.RefreshSiteList(refreshCtx); err != nil {
					log.Printf("⚠️ Fallo refresco semanal GetSiteList: %v", err)
				}
				cancel()
			}
		}
	}()
}

// durationUntilNextSunday calcula el tiempo hasta el próximo domingo a hour:minute local.
// Si hoy es domingo y aún no llegó la hora, usa hoy; si ya pasó, el domingo siguiente.
func durationUntilNextSunday(now time.Time, hour, minute int) time.Duration {
	daysUntil := (int(time.Sunday) - int(now.Weekday()) + 7) % 7
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location()).AddDate(0, 0, daysUntil)
	if !next.After(now) {
		next = next.AddDate(0, 0, 7)
	}
	return next.Sub(now)
}
