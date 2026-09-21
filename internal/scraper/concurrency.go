package scraper

import (
	"time"

	"tower-scraper/internal/concurrency"
)

func coverageConcurrencyFromEnv() int {
	return concurrency.FromEnv()
}

func newPWLimiter(max int) *pwLimiter {
	if max < 1 {
		max = 1
	}
	l := &pwLimiter{
		drain: make(chan struct{}, 1),
		sem:   make(chan struct{}, max),
	}
	l.drain <- struct{}{}
	return l
}

// pwLimiter limita operaciones Playwright concurrentes y permite bloqueo exclusivo para login/renovación.
// drain actúa como mutex con espera acotada: un canal en vez de sync.Mutex para poder rendirse
// por timeout en vez de bloquear al proceso entero si una operación previa se quedó colgada.
type pwLimiter struct {
	drain chan struct{}
	sem   chan struct{}
}

// acquire toma un hueco esperando como máximo limit. Devuelve false si no lo consigue.
func (l *pwLimiter) acquire(limit time.Duration) bool {
	timer := time.NewTimer(limit)
	defer timer.Stop()

	select {
	case <-l.drain:
	case <-timer.C:
		return false
	}
	defer func() { l.drain <- struct{}{} }()

	select {
	case l.sem <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

func (l *pwLimiter) release() {
	<-l.sem
}

// runExclusive ejecuta fn con todos los huecos tomados. Si no consigue drenarlos dentro de
// limit devuelve errPWBusy en vez de esperar indefinidamente.
func (l *pwLimiter) runExclusive(limit time.Duration, fn func() error) error {
	timer := time.NewTimer(limit)
	defer timer.Stop()

	select {
	case <-l.drain:
	case <-timer.C:
		return errPWBusy
	}
	defer func() { l.drain <- struct{}{} }()

	capacity := cap(l.sem)
	taken := 0
	defer func() {
		for i := 0; i < taken; i++ {
			<-l.sem
		}
	}()

	for taken < capacity {
		select {
		case l.sem <- struct{}{}:
			taken++
		case <-timer.C:
			return errPWBusy
		}
	}

	return fn()
}
