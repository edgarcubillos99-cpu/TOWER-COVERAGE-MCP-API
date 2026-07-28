package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"tower-scraper/internal/config"
)

// NewClient crea el cliente Redis local/Docker (cache GetSiteList) y verifica PING.
func NewClient(cfg *config.Config) (*redis.Client, error) {
	addr := cfg.RedisAddr
	if addr == "" {
		addr = "redis:6379"
	}
	return dial(addr, cfg.RedisPassword, cfg.RedisDB)
}

// NewCoverageClient crea el cliente Redis remoto para cache de coberturas.
// Si COVERAGE_REDIS_HOST está vacío, devuelve (nil, nil) — el cache de cobertura queda desactivado.
func NewCoverageClient(cfg *config.Config) (*redis.Client, error) {
	addr := cfg.CoverageRedisAddr()
	if addr == "" {
		return nil, nil
	}
	return dial(addr, cfg.CoverageRedisPassword, cfg.CoverageRedisDB)
}

func dial(addr, password string, db int) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis no disponible en %s: %w", addr, err)
	}
	return rdb, nil
}
