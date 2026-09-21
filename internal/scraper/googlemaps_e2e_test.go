package scraper

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

// TestScreenshotGoogleMapsE2E ejecuta una captura real contra Google Maps. Necesita Chromium
// instalado y salida a internet, así que solo corre con GMAPS_E2E=1:
//
//	GMAPS_E2E=1 go test ./internal/scraper/ -run E2E -v
func TestScreenshotGoogleMapsE2E(t *testing.T) {
	if os.Getenv("GMAPS_E2E") != "1" {
		t.Skip("define GMAPS_E2E=1 para ejecutar la captura real de Google Maps")
	}

	s := &TowerScraper{pwLimit: newPWLimiter(2)}
	defer s.Close()

	datos, err := s.ScreenshotGoogleMaps("18.4655", "-66.1057")
	if err != nil {
		t.Fatalf("captura de Google Maps: %v", err)
	}

	img, err := png.Decode(bytes.NewReader(datos))
	if err != nil {
		t.Fatalf("la captura no es un PNG válido: %v", err)
	}
	if b := img.Bounds(); b.Dx() != googleMapsViewportW || b.Dy() != googleMapsViewportH {
		t.Fatalf("tamaño inesperado: %dx%d", b.Dx(), b.Dy())
	}

	if err := os.WriteFile("/tmp/gmaps-e2e.png", datos, 0o644); err != nil {
		t.Logf("no se pudo guardar la captura de referencia: %v", err)
	}
}

// TestRestartBrowserE2E comprueba que un Chromium caído se relanza solo, que es lo que antes
// obligaba a reiniciar el contenedor.
func TestRestartBrowserE2E(t *testing.T) {
	if os.Getenv("GMAPS_E2E") != "1" {
		t.Skip("define GMAPS_E2E=1 para ejecutar pruebas con navegador real")
	}

	s := &TowerScraper{pwLimit: newPWLimiter(2)}
	defer s.Close()

	primero, err := s.ensureBrowser()
	if err != nil {
		t.Fatalf("arranque de Chromium: %v", err)
	}

	// Simula la caída del navegador: cerrarlo por fuera deja el puntero apuntando a algo muerto.
	if err := primero.Close(); err != nil {
		t.Fatalf("no se pudo cerrar Chromium: %v", err)
	}
	if s.browserConnected() {
		t.Fatal("el navegador debería figurar como desconectado")
	}

	segundo, err := s.ensureBrowser()
	if err != nil {
		t.Fatalf("Chromium no se relanzó: %v", err)
	}
	if !segundo.IsConnected() {
		t.Fatal("el navegador relanzado no está conectado")
	}
}
