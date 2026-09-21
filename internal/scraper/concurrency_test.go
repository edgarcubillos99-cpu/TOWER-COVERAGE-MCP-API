package scraper

import (
	"errors"
	"testing"
	"time"
)

func TestAcquireDevuelveFalseSinHuecos(t *testing.T) {
	l := newPWLimiter(1)
	if !l.acquire(time.Second) {
		t.Fatal("el primer hueco debería concederse")
	}

	inicio := time.Now()
	if l.acquire(50 * time.Millisecond) {
		t.Fatal("no debería concederse un segundo hueco con capacidad 1")
	}
	if transcurrido := time.Since(inicio); transcurrido < 50*time.Millisecond {
		t.Fatalf("acquire se rindió antes del timeout: %s", transcurrido)
	}

	l.release()
	if !l.acquire(time.Second) {
		t.Fatal("tras release el hueco debería volver a concederse")
	}
}

func TestRunExclusiveNoEsperaIndefinidamente(t *testing.T) {
	l := newPWLimiter(2)
	if !l.acquire(time.Second) {
		t.Fatal("no se pudo tomar el hueco inicial")
	}

	err := l.runExclusive(50*time.Millisecond, func() error {
		t.Fatal("fn no debe ejecutarse sin drenar todos los huecos")
		return nil
	})
	if !errors.Is(err, errPWBusy) {
		t.Fatalf("se esperaba errPWBusy, se obtuvo %v", err)
	}

	// El intento fallido debe devolver los huecos que llegó a tomar.
	l.release()
	ejecutado := false
	if err := l.runExclusive(time.Second, func() error {
		ejecutado = true
		return nil
	}); err != nil {
		t.Fatalf("runExclusive falló con el limitador libre: %v", err)
	}
	if !ejecutado {
		t.Fatal("fn no se ejecutó con el limitador libre")
	}

	if !l.acquire(time.Second) {
		t.Fatal("el limitador quedó bloqueado tras runExclusive")
	}
	l.release()
}

func TestRunExclusiveBloqueaHuecosMientrasCorre(t *testing.T) {
	l := newPWLimiter(2)
	dentro := make(chan struct{})
	salir := make(chan struct{})

	go func() {
		_ = l.runExclusive(time.Second, func() error {
			close(dentro)
			<-salir
			return nil
		})
	}()

	<-dentro
	if l.acquire(50 * time.Millisecond) {
		t.Fatal("no debería concederse un hueco durante runExclusive")
	}
	close(salir)
}
