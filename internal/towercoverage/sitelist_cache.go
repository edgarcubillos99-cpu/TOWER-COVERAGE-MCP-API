package towercoverage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const siteListRedisKey = "towercoverage:sitelist"

// SiteListStore cachea el resultado de GetSiteList en Redis.
type SiteListStore struct {
	rdb *redis.Client
	key string
}

func NewSiteListStore(rdb *redis.Client) *SiteListStore {
	return &SiteListStore{rdb: rdb, key: siteListRedisKey}
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

func (s *SiteListStore) Set(ctx context.Context, sites []Site) error {
	if s == nil || s.rdb == nil {
		return fmt.Errorf("store Redis no configurado")
	}
	raw, err := json.Marshal(sites)
	if err != nil {
		return fmt.Errorf("serializando sitelist: %w", err)
	}
	// Sin TTL: la caducidad la controla el refresco semanal (domingos).
	if err := s.rdb.Set(ctx, s.key, raw, 0).Err(); err != nil {
		return fmt.Errorf("guardando sitelist en Redis: %w", err)
	}
	return nil
}

// GetSites devuelve el listado desde Redis si existe; si no, llama a la API y lo cachea.
func (c *Client) GetSites(ctx context.Context) ([]Site, error) {
	if c.SiteStore != nil {
		sites, err := c.SiteStore.Get(ctx)
		if err != nil {
			log.Printf("⚠️ Redis sitelist: %v — se consultará la API", err)
		} else if len(sites) > 0 {
			log.Printf("GetSiteList desde Redis (%d sitios)", len(sites))
			return sites, nil
		}
	}

	sites, err := c.FetchSiteList()
	if err != nil {
		return nil, err
	}
	if c.SiteStore != nil {
		if err := c.SiteStore.Set(ctx, sites); err != nil {
			log.Printf("⚠️ No se pudo cachear sitelist en Redis: %v", err)
		} else {
			log.Printf("GetSiteList cacheado en Redis (%d sitios)", len(sites))
		}
	}
	return sites, nil
}

// RefreshSiteList fuerza un GetSiteList a la API y actualiza Redis.
func (c *Client) RefreshSiteList(ctx context.Context) error {
	sites, err := c.FetchSiteList()
	if err != nil {
		return err
	}
	if c.SiteStore == nil {
		return fmt.Errorf("store Redis no configurado")
	}
	if err := c.SiteStore.Set(ctx, sites); err != nil {
		return err
	}
	log.Printf("GetSiteList refrescado en Redis (%d sitios)", len(sites))
	return nil
}

// EnsureSiteListCache rellena Redis si está vacío (arranque del servicio).
func (c *Client) EnsureSiteListCache(ctx context.Context) error {
	if c.SiteStore == nil {
		return nil
	}
	sites, err := c.SiteStore.Get(ctx)
	if err != nil {
		return err
	}
	if len(sites) > 0 {
		log.Printf("Redis ya tiene sitelist (%d sitios); no se llama a GetSiteList", len(sites))
		return nil
	}
	log.Println("Redis sin sitelist; obteniendo GetSiteList de la API...")
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
