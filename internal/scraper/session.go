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

// TowerCoverage ha usado id="Username" y id="UserName" según versión del sitio.
var loginUsernameSelectors = []string{"#Username", "#UserName"}

// El botón de login pasó de input[type=submit] a button#signInBtn.
var loginButtonSelectors = []string{
	"#signInBtn",
	`#loginForm button[type="submit"]`,
	`button.tc-submit-btn`,
	`input[type="submit"][value="Login"]`,
}

const loginFieldTimeoutMs = 15000

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
	if _, err = page.Goto(loginURL, playwright.PageGotoOptions{
		Timeout: playwright.Float(60000),
	}); err != nil {
		log.Printf("[Login] fallo al navegar a la URL de login: %v", err)
		return fmt.Errorf("error navegando al login: %v", err)
	}

	if err := fillFirstLocator(page, loginUsernameSelectors, s.tcUser, "username"); err != nil {
		log.Printf("[Login] fallo al rellenar usuario: %v", err)
		_, _ = page.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String("error_login_username.png"),
		})
		return err
	}
	if err := page.Locator("#Password").Fill(s.tcPass, playwright.LocatorFillOptions{
		Timeout: playwright.Float(loginFieldTimeoutMs),
	}); err != nil {
		log.Printf("[Login] fallo al rellenar contraseña: %v", err)
		return fmt.Errorf("error llenando password: %v", err)
	}

	if err := clickFirstLocator(page, loginButtonSelectors, "clic en botón Login"); err != nil {
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
	// Exportamos las cookies recién obtenidas al cliente HTTP nativo para que las consultas
	// JSON no dependan del navegador. Si falla, se registra pero no aborta el login.
	if err := s.syncCookiesToHTTPClient(); err != nil {
		log.Printf("[Login] advertencia sincronizando cookies al cliente HTTP: %v", err)
	}
	log.Println("Login exitoso. Sesión guardada en el contexto.")
	return nil
}

func sessionExpiredOnPage(page playwright.Page) bool {
	u := strings.ToLower(page.URL())
	if strings.Contains(u, "/login") {
		return true
	}
	return loginUsernameFieldVisible(page)
}

func fillFirstLocator(page playwright.Page, selectors []string, value, fieldName string) error {
	var lastErr error
	for _, sel := range selectors {
		loc := page.Locator(sel)
		fillErr := loc.Fill(value, playwright.LocatorFillOptions{
			Timeout: playwright.Float(loginFieldTimeoutMs),
		})
		if fillErr == nil {
			log.Printf("[Login] campo %s rellenado con selector %s", fieldName, sel)
			return nil
		}
		lastErr = fillErr
		log.Printf("[Login] selector %s no disponible para %s: %v", sel, fieldName, fillErr)
	}
	return fmt.Errorf("error llenando %s (probados %v): %v", fieldName, selectors, lastErr)
}

func clickFirstLocator(page playwright.Page, selectors []string, actionName string) error {
	var lastErr error
	for _, sel := range selectors {
		loc := page.Locator(sel)
		clickErr := loc.Click(playwright.LocatorClickOptions{
			Timeout: playwright.Float(loginFieldTimeoutMs),
		})
		if clickErr == nil {
			log.Printf("[Login] %s con selector %s", actionName, sel)
			return nil
		}
		lastErr = clickErr
		log.Printf("[Login] selector %s no disponible para %s: %v", sel, actionName, clickErr)
	}
	return fmt.Errorf("error en %s (probados %v): %v", actionName, selectors, lastErr)
}

func loginUsernameFieldVisible(page playwright.Page) bool {
	for _, sel := range loginUsernameSelectors {
		loc := page.Locator(sel)
		n, err := loc.Count()
		if err != nil || n == 0 {
			continue
		}
		vis, err := loc.First().IsVisible()
		if err == nil && vis {
			return true
		}
	}
	return false
}
