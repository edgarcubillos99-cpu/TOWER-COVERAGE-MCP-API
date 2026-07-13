package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"tower-scraper/internal/config"

	"github.com/redis/go-redis/v9"
)

// warmMarkerKey guarda la marca de tiempo de la última precarga completa.
const warmMarkerKey = "tower_aps:__warmed_at__"

// InitRedis conecta con Redis usando la configuración. Si RedisAddr está vacío, no hace
// nada y la caché queda deshabilitada (todo consulta MySQL). Un fallo de conexión NO es
// fatal: se registra una advertencia y el sistema sigue funcionando solo con MySQL.
func (c *DBClient) InitRedis(cfg *config.Config) error {
	if strings.TrimSpace(cfg.RedisAddr) == "" {
		log.Println("Redis deshabilitado (REDIS_ADDR vacío); las consultas de APs irán directo a MySQL.")
		return nil
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		log.Printf("⚠️ No se pudo conectar a Redis en %s: %v. Se continúa solo con MySQL.", cfg.RedisAddr, err)
		return nil
	}

	c.rdb = rdb
	if cfg.RedisTTLMinutes > 0 {
		c.cacheTTL = time.Duration(cfg.RedisTTLMinutes) * time.Minute
	}
	log.Printf("Redis conectado en %s (DB %d, TTL %s).", cfg.RedisAddr, cfg.RedisDB, ttlLabel(c.cacheTTL))
	return nil
}

func ttlLabel(ttl time.Duration) string {
	if ttl <= 0 {
		return "sin expiración"
	}
	return ttl.String()
}

// CacheEnabled indica si Redis está activo.
func (c *DBClient) CacheEnabled() bool {
	return c != nil && c.rdb != nil
}

// WarmCache precarga en Redis todos los APs agrupados por torre. Devuelve la cantidad de
// torres cacheadas y el total de APs. Es idempotente: sobreescribe las claves existentes.
func (c *DBClient) WarmCache(ctx context.Context) (torres int, aps int, err error) {
	if !c.CacheEnabled() {
		return 0, 0, nil
	}

	grupos, total, err := c.cargarTodosLosAPsAgrupados()
	if err != nil {
		return 0, 0, err
	}

	pipe := c.rdb.Pipeline()
	for key, lista := range grupos {
		payload, mErr := json.Marshal(lista)
		if mErr != nil {
			log.Printf("[WarmCache] no se pudo serializar APs de %q: %v", key, mErr)
			continue
		}
		pipe.Set(ctx, key, payload, c.cacheTTL)
	}
	pipe.Set(ctx, warmMarkerKey, time.Now().Format(time.RFC3339), c.cacheTTL)

	if _, execErr := pipe.Exec(ctx); execErr != nil {
		return 0, 0, fmt.Errorf("error escribiendo caché en Redis: %w", execErr)
	}

	return len(grupos), total, nil
}

// cargarTodosLosAPsAgrupados lee toda la tabla dispositivos_ap y agrupa los APs por la clave
// de caché derivada de torre_nombre. También consulta la tabla torres para registrar cuántas
// torres existen (contexto/log), aunque el pipeline de cobertura solo consume los APs.
func (c *DBClient) cargarTodosLosAPsAgrupados() (map[string][]APInfo, int, error) {
	if torres, err := c.contarTorres(); err == nil {
		log.Printf("[WarmCache] torres en BD: %d", torres)
	}

	query := `SELECT torre_nombre, ap_name, azimut, tilt, altura, tipo, ip_address FROM dispositivos_ap`
	rows, err := c.conn.Query(query)
	if err != nil {
		return nil, 0, fmt.Errorf("error leyendo dispositivos_ap para caché: %w", err)
	}
	defer rows.Close()

	grupos := make(map[string][]APInfo)
	total := 0
	for rows.Next() {
		var torreNombre string
		var ap APInfo
		var azimut, tilt, altura, ip sql.NullString
		if err := rows.Scan(&torreNombre, &ap.APName, &azimut, &tilt, &altura, &ap.Tipo, &ip); err != nil {
			return nil, 0, err
		}
		ap.Azimut = nullStringValue(azimut)
		ap.Tilt = nullStringValue(tilt)
		ap.Altura = nullStringValue(altura)
		if ip.Valid {
			ap.IPAddress = strings.TrimSpace(ip.String)
		}
		key := cacheKeyTowerName(cleanTowerNameForDB(torreNombre))
		grupos[key] = append(grupos[key], ap)
		total++
	}
	return grupos, total, rows.Err()
}

func (c *DBClient) contarTorres() (int, error) {
	var n int
	err := c.conn.QueryRow(`SELECT COUNT(*) FROM torres`).Scan(&n)
	return n, err
}

// apsFromCache intenta leer los APs de una torre desde Redis. El segundo valor es false si
// la caché está deshabilitada, la clave no existe o hubo cualquier error (para caer a MySQL).
func (c *DBClient) apsFromCache(key string) ([]APInfo, bool) {
	if !c.CacheEnabled() {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	val, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		// redis.Nil (miss) o cualquier error: se resuelve con MySQL.
		return nil, false
	}
	var aps []APInfo
	if err := json.Unmarshal(val, &aps); err != nil {
		log.Printf("[cache] valor corrupto en %q: %v; se consultará MySQL", key, err)
		return nil, false
	}
	return aps, true
}

// storeAPsInCache guarda el resultado de una consulta a MySQL para acelerar próximas lecturas.
// Cachea también listas vacías (caché negativa) para no repetir consultas de torres sin APs.
func (c *DBClient) storeAPsInCache(key string, aps []APInfo) {
	if !c.CacheEnabled() {
		return
	}
	payload, err := json.Marshal(aps)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.rdb.Set(ctx, key, payload, c.cacheTTL).Err(); err != nil {
		log.Printf("[cache] no se pudo guardar %q en Redis: %v", key, err)
	}
}

// Close libera la conexión a Redis si está activa.
func (c *DBClient) Close() error {
	if c.rdb != nil {
		return c.rdb.Close()
	}
	return nil
}
