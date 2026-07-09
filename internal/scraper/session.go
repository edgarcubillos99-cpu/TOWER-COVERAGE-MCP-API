package scraper

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const defaultSessionMaxAge = 4 * time.Hour

func sessionMaxAgeFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("TOWER_SESSION_MAX_AGE_MINUTES")); v != "" {
		if m, err := strconv.Atoi(v); err == nil && m > 0 {
			return time.Duration(m) * time.Minute
		}
	}
	return defaultSessionMaxAge
}

func sessionKeepaliveFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("TOWER_SESSION_KEEPALIVE_MINUTES")); v != "" {
		if m, err := strconv.Atoi(v); err == nil && m > 0 {
			return time.Duration(m) * time.Minute
		}
	}
	return 0
}

// StartSessionKeeper renueva la sesión en segundo plano si TOWER_SESSION_KEEPALIVE_MINUTES > 0.
func (s *TowerScraper) StartSessionKeeper() {
	interval := sessionKeepaliveFromEnv()
	if interval <= 0 {
		return
	}
	log.Printf("Renovación periódica de sesión TowerCoverage cada %s", interval.Round(time.Minute))
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			age := time.Since(s.sessionStarted)
			log.Printf("Keepalive: renovando sesión TowerCoverage (antigüedad %s)...", age.Round(time.Second))
			if err := s.runPWExclusive(func() error {
				s.loginMu.Lock()
				defer s.loginMu.Unlock()
				return s.loginUnderLock()
			}); err != nil {
				log.Printf("⚠️ Keepalive: fallo renovando sesión TowerCoverage: %v", err)
			}
		}
	}()
}

func (s *TowerScraper) ensureSession() error {
	if err := s.ensureBrowser(); err != nil {
		return err
	}

	s.loginMu.Lock()
	needsLogin := s.context == nil
	maxAge := sessionMaxAgeFromEnv()
	needsRenew := !needsLogin && !s.sessionStarted.IsZero() && time.Since(s.sessionStarted) >= maxAge
	s.loginMu.Unlock()

	if !needsLogin && !needsRenew {
		return nil
	}

	return s.runPWExclusive(func() error {
		s.loginMu.Lock()
		defer s.loginMu.Unlock()

		if s.context == nil {
			log.Println("Sin contexto de navegador; iniciando sesión TowerCoverage...")
			return s.loginUnderLock()
		}

		if !s.sessionStarted.IsZero() && time.Since(s.sessionStarted) < maxAge {
			return nil
		}

		log.Printf("Sesión TowerCoverage antigua (%s); renovando antes de continuar...",
			time.Since(s.sessionStarted).Round(time.Minute))
		return s.loginUnderLock()
	})
}

func (s *TowerScraper) renewSession() error {
	return s.runPWExclusive(func() error {
		s.loginMu.Lock()
		defer s.loginMu.Unlock()
		return s.loginUnderLock()
	})
}

// loginUnderLock ejecuta el flujo de login. Requiere loginMu tomado por el llamador.
func (s *TowerScraper) loginUnderLock() error {
	if s.tcUser == "" || s.tcPass == "" {
		return fmt.Errorf("credenciales TowerCoverage no configuradas")
	}

	if s.context != nil {
		_ = s.context.Close()
		s.context = nil
	}

	context, err := s.browser.NewContext()
	if err != nil {
		log.Printf("[Login] fallo al crear contexto del navegador: %v", err)
		return err
	}
	s.context = context

	page, err := context.NewPage()
	if err != nil {
		log.Printf("[Login] fallo al abrir pestaña de login: %v", err)
		return err
	}
	defer page.Close()

	loginURL := "https://www.towercoverage.com/Login"
	if _, err = page.Goto(loginURL); err != nil {
		log.Printf("[Login] fallo al navegar a la URL de login: %v", err)
		return fmt.Errorf("error navegando al login: %v", err)
	}

	if err := page.Locator("#UserName").Fill(s.tcUser); err != nil {
		log.Printf("[Login] fallo al rellenar usuario: %v", err)
		return fmt.Errorf("error llenando username: %v", err)
	}
	if err := page.Locator("#Password").Fill(s.tcPass); err != nil {
		log.Printf("[Login] fallo al rellenar contraseña: %v", err)
		return fmt.Errorf("error llenando password: %v", err)
	}

	loginBtn := page.Locator(`input[type="submit"][value="Login"]`)
	if err := loginBtn.Click(); err != nil {
		log.Printf("[Login] fallo al hacer clic en el botón Login: %v", err)
		_, _ = page.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String("error_login_click.png"),
		})
		return fmt.Errorf("error haciendo click en el botón Login: %v", err)
	}

	signOutBtn := page.GetByText("Sign Out", playwright.PageGetByTextOptions{Exact: playwright.Bool(true)})
	if err := signOutBtn.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		log.Printf("[Login] fallo esperando texto \"Sign Out\" (timeout o no visible): %v", err)
		return fmt.Errorf("el dashboard no cargó a tiempo tras el login: %v", err)
	}

	if page.URL() == loginURL {
		log.Printf("[Login] fallo de validación: seguimos en la URL de login")
		_, _ = page.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String("error_credenciales.png"),
		})
		return fmt.Errorf("login fallido: posibles credenciales incorrectas, seguimos en la pantalla de login")
	}

	s.sessionStarted = time.Now()
	log.Println("Login exitoso. Sesión guardada en el contexto.")
	return nil
}

func sessionExpiredOnPage(page playwright.Page) bool {
	u := strings.ToLower(page.URL())
	if strings.Contains(u, "/login") {
		return true
	}
	loc := page.Locator("#UserName")
	n, err := loc.Count()
	if err != nil || n == 0 {
		return false
	}
	vis, err := loc.First().IsVisible()
	return err == nil && vis
}
