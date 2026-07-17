package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"tower-scraper/internal/config"
)

// NewClient crea un cliente Redis y verifica la conexión con PING.
func NewClient(cfg *config.Config) (*redis.Client, error) {
	addr := cfg.RedisAddr
	if addr == "" {
		addr = "redis:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis no disponible en %s: %w", addr, err)
	}
	return rdb, nil
}
