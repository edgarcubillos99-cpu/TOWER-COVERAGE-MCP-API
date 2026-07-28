package scraper

import (
	"sync"

	"tower-scraper/internal/concurrency"
)

func coverageConcurrencyFromEnv() int {
	return concurrency.FromEnv()
}

func newPWLimiter(max int) *pwLimiter {
	if max < 1 {
		max = 1
	}
	return &pwLimiter{sem: make(chan struct{}, max)}
}

// pwLimiter limita operaciones Playwright concurrentes y permite bloqueo exclusivo para login/renovación.
type pwLimiter struct {
	drain sync.Mutex
	sem   chan struct{}
}

func (l *pwLimiter) acquire() {
	l.drain.Lock()
	l.sem <- struct{}{}
	l.drain.Unlock()
}

func (l *pwLimiter) release() {
	<-l.sem
}

func (l *pwLimiter) runExclusive(fn func() error) error {
	l.drain.Lock()
	defer l.drain.Unlock()

	capacity := cap(l.sem)
	for i := 0; i < capacity; i++ {
		l.sem <- struct{}{}
	}
	defer func() {
		for i := 0; i < capacity; i++ {
			<-l.sem
		}
	}()

	return fn()
}
