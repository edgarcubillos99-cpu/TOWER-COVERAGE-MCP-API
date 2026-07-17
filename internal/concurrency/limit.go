package concurrency

import (
	"os"
	"strconv"
)

const Default = 15

// FromEnv lee TOWER_COVERAGE_CONCURRENCY (LinkPathAPI, coords /full, Playwright legado).
// Default 15; valores inválidos o <= 0 caen al default.
func FromEnv() int {
	if v := os.Getenv("TOWER_COVERAGE_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return Default
}
