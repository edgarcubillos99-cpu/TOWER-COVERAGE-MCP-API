package scraper

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"tower-scraper/internal/config"
	"tower-scraper/internal/db"
	"tower-scraper/internal/geo"
	"tower-scraper/internal/models"
	"tower-scraper/internal/redisx"
	"tower-scraper/internal/snmp"
	"tower-scraper/internal/towercoverage"

	"github.com/mxschmitt/playwright-go"
	"github.com/redis/go-redis/v9"
)

// skipRFConeWebRender evita la fase lenta de Playwright en EditCoverages (búsqueda de cliente,
// beamwidth, #showFilter, zoom y captura del mapa con el cono RF). La cobertura se determina
// solo con trigonometría (azimut AP vs bearing al cliente). Pon en false para restaurar el flujo anterior.
const skipRFConeWebRender = true

var reNumerosAzimutAltura = regexp.MustCompile(`[^\d.]`)

type TowerScraper struct {
	pw             *playwright.Playwright
	browser        playwright.Browser
	context        playwright.BrowserContext
	tcUser         string
	tcPass         string
	sessionStarted time.Time
	apiClient      *towercoverage.Client
	redis          *redis.Client // Redis local/Docker: GetSiteList
	coverageRedis  *redis.Client // Redis remoto: resultados de cobertura por proximidad
	browserMu      sync.Mutex
	// pwLimit limita operaciones Playwright concurrentes (Google Maps, flujo legado web).
	pwLimit *pwLimiter
	// loginMu protege login/renovación y el reemplazo del BrowserContext.
	loginMu sync.Mutex
}

// NewTowerScraper prepara el cliente de cobertura vía API. Playwright se inicia bajo demanda
// (p. ej. get_google_maps_screenshot); no hace falta en el arranque ni en el build Docker.
func NewTowerScraper(cfg *config.Config) (*TowerScraper, error) {
	conc := coverageConcurrencyFromEnv()
	log.Printf("Consultas concurrentes: hasta %d (TOWER_COVERAGE_CONCURRENCY); cobertura vía API REST", conc)

	apiClient := towercoverage.NewClient(cfg.APIAccount, cfg.APIKey)

	rdb, err := redisx.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("redis local (sitelist): %w", err)
	}
	apiClient.SiteStore = towercoverage.NewSiteListStore(rdb)
	log.Printf("Redis local conectado en %s (cache GetSiteList)", cfg.RedisAddr)

	covRDB, err := redisx.NewCoverageClient(cfg)
	if err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis remoto (coberturas): %w", err)
	}
	if covRDB != nil {
		log.Printf("Redis remoto conectado en %s (cache coberturas por proximidad)", cfg.CoverageRedisAddr())
	} else {
		log.Println("⚠️ COVERAGE_REDIS_HOST no definido: cache de coberturas por proximidad desactivado")
	}

	return &TowerScraper{
		pwLimit:       newPWLimiter(conc),
		apiClient:     apiClient,
		redis:         rdb,
		coverageRedis: covRDB,
	}, nil
}

// APIClient expone el cliente TowerCoverage (cache sitelist / refresco semanal).
func (s *TowerScraper) APIClient() *towercoverage.Client {
	return s.apiClient
}

// Redis expone el Redis local/Docker (cache GetSiteList).
func (s *TowerScraper) Redis() *redis.Client {
	return s.redis
}

// CoverageRedis expone el Redis remoto (cache de coberturas procesadas).
func (s *TowerScraper) CoverageRedis() *redis.Client {
	return s.coverageRedis
}

func (s *TowerScraper) ensureBrowser() error {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if s.browser != nil {
		return nil
	}

	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("playwright no disponible (instala Chromium con playwright install): %w", err)
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		pw.Stop()
		return fmt.Errorf("no se pudo lanzar Chromium: %w", err)
	}

	s.pw = pw
	s.browser = browser
	log.Println("Playwright/Chromium inicializado bajo demanda.")
	return nil
}

func (s *TowerScraper) acquirePWSlot() {
	s.pwLimit.acquire()
}

func (s *TowerScraper) releasePWSlot() {
	s.pwLimit.release()
}

func (s *TowerScraper) runPWExclusive(fn func() error) error {
	return s.pwLimit.runExclusive(fn)
}

// Login maneja la autenticación y guarda la sesión.
func (s *TowerScraper) Login(username, password string) error {
	s.tcUser = username
	s.tcPass = password
	log.Println("Iniciando proceso de Login...")
	return s.runPWExclusive(func() error {
		s.loginMu.Lock()
		defer s.loginMu.Unlock()
		return s.loginUnderLock()
	})
}

// GetTowersData consulta GetSiteList, filtra por distancia y existencia en BD torres,
// luego evalúa LinkPathAPI y conserva enlaces posibles.
func (s *TowerScraper) GetTowersData(dbClient *db.DBClient, lat, lon string) ([]models.TowerCoverage, error) {
	log.Printf("Consultando cobertura vía API (GetSiteList/LinkPath) para Lat: %s, Lon: %s...", lat, lon)
	return towercoverage.ResolveTowers(dbClient, s.apiClient, lat, lon)
}

// TestAPCoverage navega a Coverages, busca la torre, entra en ella y simula la configuración de sus APs
func (s *TowerScraper) TestAPCoverage(torre models.TowerCoverage, aps []db.APInfo, latCliente, lonCliente string) ([]models.RespuestaMCP, error) {
	if !skipRFConeWebRender {
		if err := s.ensureSession(); err != nil {
			return nil, fmt.Errorf("sesión TowerCoverage: %w", err)
		}
		s.acquirePWSlot()
		defer s.releasePWSlot()
	}

	towerName := torre.TowerName
	safeName := strings.ReplaceAll(towerName, " ", "_")
	safeName = strings.ReplaceAll(safeName, "/", "-")

	log.Printf("Iniciando validación en la torre: %s para %d APs", towerName, len(aps))

	var towerURL string
	if skipRFConeWebRender {
		log.Printf("Modo rápido: se omite Coverages + render del cono RF en web; solo trigonometría concurrente.")
	} else {
		page, err := s.context.NewPage()
		if err != nil {
			return nil, fmt.Errorf("error creando página de validación: %v", err)
		}
		// No usamos defer page.Close() aquí para poder cerrarla anticipadamente y liberar RAM

		if _, err = page.Goto("https://www.towercoverage.com/En-US/Coverages", playwright.PageGotoOptions{
			Timeout: playwright.Float(60000),
		}); err != nil {
			return nil, fmt.Errorf("error navegando a Coverages: %v", err)
		}

		searchLocator := page.Locator("input.tablesorter-filter[data-column='1']").First()
		if err := searchLocator.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		}); err != nil {
			return nil, fmt.Errorf("error esperando input de búsqueda en coverages: %w", err)
		}

		if err := searchLocator.Fill(towerName); err != nil {
			return nil, fmt.Errorf("error escribiendo en input: %w", err)
		}
		searchLocator.Press("Enter")
		page.WaitForTimeout(1500)

		rowLocators := page.Locator("tr[role='row']:not(.tablesorter-filter-row)")
		count, err := rowLocators.Count()
		if err != nil || count == 0 {
			return nil, fmt.Errorf("sin filas de datos para la torre %q", towerName)
		}

		var selectedRow playwright.Locator
		for i := 0; i < count; i++ {
			row := rowLocators.Nth(i)
			listNameText, _ := row.Locator("td.listName").InnerText()
			listNameText = strings.TrimSpace(listNameText)

			if strings.Contains(strings.ToLower(listNameText), strings.ToLower(towerName)) {
				selectedRow = row
				break
			}
		}

		if selectedRow == nil {
			return nil, fmt.Errorf("sin coincidencia de texto en tabla para %q", towerName)
		}

		// Extraer la URL directa de la torre
		editLink := selectedRow.Locator("td.listName a").First()
		href, err := editLink.GetAttribute("href")
		if err != nil {
			return nil, fmt.Errorf("no se pudo extraer la URL de la torre: %v", err)
		}
		towerURL = "https://www.towercoverage.com" + href

		// Cerramos la pestaña general, ya no la necesitamos. Cada worker abrirá la suya.
		page.Close()
		log.Printf("URL directa obtenida: %s. Iniciando Worker Pool...", towerURL)
	}

	var resultadosFinales []models.RespuestaMCP
	var wg sync.WaitGroup
	var mu sync.Mutex

	type job struct {
		Index int
		AP    db.APInfo
	}
	jobs := make(chan job, len(aps))
	numWorkers := 10

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := range jobs {
				res := s.processSingleAP(workerID, towerURL, safeName, j.AP, latCliente, lonCliente, torre, j.Index)
				mu.Lock()
				resultadosFinales = append(resultadosFinales, res)
				mu.Unlock()
			}
		}(w)
	}

	for i, ap := range aps {
		jobs <- job{Index: i, AP: ap}
	}
	close(jobs)
	wg.Wait()

	log.Printf("✅ Todos los %d APs fueron validados concurrentemente.", len(resultadosFinales))
	return resultadosFinales, nil
}

// processSingleAP abre una pestaña propia, va directo a la torre y prueba un solo AP
func (s *TowerScraper) processSingleAP(workerID int, towerURL, safeName string, ap db.APInfo, latCliente, lonCliente string, torre models.TowerCoverage, i int) models.RespuestaMCP {
	log.Printf("[Worker-%d] Iniciando AP [%d] -> Nombre: %s", workerID, i+1, ap.APName)
	startWorker := time.Now()

	// Estructura base para retornos en caso de error prematuro
	respuestaBase := models.RespuestaMCP{
		Antena:      ap.APName,
		Tipo:        ap.Tipo,
		Cobertura:   false,
		NombreTorre: torre.TowerName,
	}
	respuestaBase.ApplySiteFields(torre)

	beamwidth := geo.ObtenerApertura(ap.Tipo, ap.APName)

	var alignExtraido string
	var statusExtraido string

	if !skipRFConeWebRender {
		page, err := s.context.NewPage()
		if err != nil {
			log.Printf("[Worker-%d] Error creando pestaña para %s: %v", workerID, ap.APName, err)
			ap.Status = "Error de Pestaña Playwright"
			return respuestaBase
		}
		defer page.Close()

		// Timeout por defecto bajo para que ningún locator cuelgue 30s reintentando actionability
		page.SetDefaultTimeout(8000)

		if _, err := page.Goto(towerURL, playwright.PageGotoOptions{
			Timeout:   playwright.Float(60000),
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		}); err != nil {
			log.Printf("[Worker-%d] Error navegando para %s: %v", workerID, ap.APName, err)
			ap.Status = "Error cargando URL"
			return respuestaBase
		}

		// 1) Coordenadas
		if err := page.Locator("#address").Fill(fmt.Sprintf("%s, %s", latCliente, lonCliente)); err != nil {
			log.Printf("[Worker-%d] fallo #address para %s: %v", workerID, ap.APName, err)
		}
		if err := page.Locator("input.newbutton[value='Search']").Click(); err != nil {
			log.Printf("[Worker-%d] fallo click Search para %s: %v", workerID, ap.APName, err)
		}
		// La búsqueda repinta el mapa y a veces rehace el formulario; esperar red antes de tocar parámetros.
		_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   playwright.LoadStateNetworkidle,
			Timeout: playwright.Float(15000),
		})

		// Hay que elegir alguna opción del sistema de radio (índice 1 = primera opción distinta del placeholder).
		if _, err := page.Locator("#RadioSystemList").SelectOption(playwright.SelectOptionValues{Indexes: &[]int{1}}); err != nil {
			log.Printf("[Worker-%d] fallo SelectOption RadioSystem para %s: %v", workerID, ap.APName, err)
		}
		_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   playwright.LoadStateNetworkidle,
			Timeout: playwright.Float(12000),
		})

		// 2) Antenna Height → Beamwidth (90) → Azimuth al final. El beamwidth puede disparar recálculo del formulario
		// y dejar el azimut en 0 si lo rellenamos antes; por eso no se llama a #showFilter hasta que #Azimuth coincida con la BD.
		alturaLimpia := reNumerosAzimutAltura.ReplaceAllString(ap.Altura, "")
		if alturaLimpia != "" {
			alturaInput := page.Locator("#AntennaHeightfeet")
			if err := alturaInput.WaitFor(playwright.LocatorWaitForOptions{
				State:   playwright.WaitForSelectorStateVisible,
				Timeout: playwright.Float(4000),
			}); err == nil {
				_ = alturaInput.Fill(alturaLimpia)
				_ = alturaInput.Blur()
			} else {
				_, _ = page.Evaluate(fmt.Sprintf(`document.getElementById("AntennaHeightfeet").value = "%s";`, alturaLimpia))
			}
		}

		setBeamwidthFilterFixed(page, strconv.FormatFloat(beamwidth, 'f', 0, 64))
		_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   playwright.LoadStateNetworkidle,
			Timeout: playwright.Float(10000),
		})

		azimuthLimpio := ""
		if azimutFloat, azimutOK := geo.ParsearAzimut(ap.Azimut); azimutOK {
			azimuthLimpio = strconv.FormatFloat(azimutFloat, 'f', -1, 64)
		}
		if azimuthLimpio != "" {
			if !ensureAzimuthCommitted(page, workerID, ap.APName, azimuthLimpio) {
				log.Printf("[Worker-%d] azimut no confirmado para %s (esperado %q); se omite #showFilter", workerID, ap.APName, azimuthLimpio)

				rutaError := fmt.Sprintf("./capturas/%s_AP_%d_%s_error.png", safeName, i, ap.APName)
				if _, err := page.Screenshot(playwright.PageScreenshotOptions{
					Path:     playwright.String(rutaError),
					FullPage: playwright.Bool(true),
				}); err != nil {
					log.Printf("[Worker-%d] fallo screenshot tras fallo azimut %s: %v", workerID, ap.APName, err)
				}
				return respuestaBase
			}
		}

		// 3) Ejecutar y esperar a que el render RF termine usando 'networkidle' en vez de sleep ciego.
		if err := page.Locator("#showFilter").Click(); err != nil {
			log.Printf("[Worker-%d] fallo click #showFilter para %s: %v", workerID, ap.APName, err)
		}
		_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   playwright.LoadStateNetworkidle,
			Timeout: playwright.Float(20000),
		})

		// 4) ALEJAR EL MAPA (ZOOM OUT)
		t0 := time.Now()
		if ok := zoomOutMap(page, 2); ok {
			log.Printf("[Worker-%d] Zoom out OK para %s en %s", workerID, ap.APName, time.Since(t0).Round(time.Millisecond))
		} else {
			log.Printf("[Worker-%d] No se pudo alejar el mapa para %s (sin botón reconocible)", workerID, ap.APName)
		}
		page.WaitForTimeout(1200)

		// EXTRACCIÓN, CAPTURA (cono en mapa web) — la cobertura efectiva sigue decidiéndose abajo con trigonometría.

		alignExtraido = torre.Alignment
		statusExtraido = "Validación visual generada"

		// Captura del mapa con el cono (legado / depuración); el flag skipRFConeWebRender evita todo este bloque.
		rutaScreenshot := fmt.Sprintf("./capturas/%s_AP_%d_%s_resultado.png", safeName, i, ap.APName)
		if _, err := page.Screenshot(playwright.PageScreenshotOptions{
			Path:     playwright.String(rutaScreenshot),
			FullPage: playwright.Bool(true),
		}); err != nil {
			log.Printf("[Worker-%d] fallo screenshot para %s: %v", workerID, ap.APName, err)
			statusExtraido = "Fallo al capturar imagen"
		}
		_ = rutaScreenshot // reservado si se reactiva análisis por imagen (GoCV)
	} else {
		alignExtraido = torre.Alignment
		statusExtraido = "Cobertura por trigonometría (sin render del cono RF en web)"
	}

	// C. Calcular Distancia Matemática usando los datos que vienen desde GetTowersData
	latClienteFloat, _ := strconv.ParseFloat(latCliente, 64)
	lonClienteFloat, _ := strconv.ParseFloat(lonCliente, 64)
	latTorreFloat, _ := strconv.ParseFloat(torre.Latitude, 64)
	lonTorreFloat, _ := strconv.ParseFloat(torre.Longitude, 64)

	distanciaKm := geo.CalcularDistancia(latTorreFloat, lonTorreFloat, latClienteFloat, lonClienteFloat)

	// D. Analizar la imagen con GoCV
	/*
		coberturaViable, errVision := vision.AnalizarCobertura(rutaScreenshot)
		if errVision != nil {
			log.Printf("[Worker-%d] Error analizando visión en %s: %v", workerID, ap.APName, errVision)
			coberturaViable = false
			statusExtraido = "Error en análisis visual"
		}
	*/

	// D. Cálculo matemático de cobertura (beamwidth según tipo Wave/Wabe u otros)
	azimutFloat, azimutOK := geo.ParsearAzimut(ap.Azimut)
	bearingCliente := geo.CalcularAngulo(latTorreFloat, lonTorreFloat, latClienteFloat, lonClienteFloat)

	coberturaViable := geo.EstaEnCobertura(azimutFloat, bearingCliente, beamwidth)
	if !coberturaViable && !azimutOK && beamwidth < 360 {
		statusExtraido = strings.TrimSpace(statusExtraido + "; azimut no disponible — requiere verificación manual")
	}

	log.Printf("[Worker-%d] ✅ AP [%d] %s. Dist: %.2f km | Azimut AP: %.2f° (ok=%t) | Ángulo Cliente: %.2f° | Beamwidth: %.0f° | En Cono: %t",
		workerID, i+1, ap.APName, distanciaKm, azimutFloat, azimutOK, bearingCliente, beamwidth, coberturaViable)

	log.Printf("[Worker-%d] ✅ AP [%d] %s listo en %s. Cobertura: %t, Distancia: %.2f km", workerID, i+1, ap.APName, time.Since(startWorker).Round(time.Second), coberturaViable, distanciaKm)

	_ = alignExtraido
	_ = statusExtraido

	// E. Retornar JSON plano para el agente IA
	out := models.RespuestaMCP{
		Antena:      ap.APName,
		Tipo:        ap.Tipo,
		Distancia:   distanciaKm,
		Cobertura:   coberturaViable,
		NombreTorre: torre.TowerName,
	}
	out.ApplySiteFields(torre)

	if coberturaViable && strings.TrimSpace(ap.IPAddress) != "" {
		st, err := snmp.CheckSaturation(models.AccessPoint{
			TowerName: torre.TowerName,
			APName:    ap.APName,
			Tipo:      ap.Tipo,
			IPAddress: strings.TrimSpace(ap.IPAddress),
		})
		if err != nil {
			log.Printf("[Worker-%d] SNMP clientes para %s: %v", workerID, ap.APName, err)
		} else {
			n := st.Clients
			out.ClientesConectados = &n
			applySNMPStatusToRespuesta(&out, st)
		}
	} else if coberturaViable {
		log.Printf("[Worker-%d] Cobertura OK pero sin IP en BD para %s; se omite SNMP", workerID, ap.APName)
	}

	return out
}

// applySNMPStatusToRespuesta copia saturación y texto de diagnóstico al JSON MCP.
// esta_saturado solo se rellena cuando hubo OID válido y EvaluateAP aplicó el umbral (25 clientes).
func applySNMPStatusToRespuesta(out *models.RespuestaMCP, st models.APStatus) {
	msg := strings.TrimSpace(st.Message)
	if cap := strings.TrimSpace(st.EstadoCapacidad); cap != "" {
		out.EstadoCapacidad = cap
	} else if msg != "" {
		out.EstadoCapacidad = msg
	}
	if msg == "Saturado" || msg == "Con espacio" {
		out.EstaSaturado = st.IsSaturated
	}
}

// zoomOutMap aleja el mapa probando, en orden, lo que realmente mueve el widget del mapa:
// 1) Controles nativos (Google Maps / Leaflet).
// 2) Enlace o botón accesible "Zoom Out" exacto (evita div:has-text que matchea contenedores enormes y el clic no hace nada).
// 3) Rueda del ratón en el centro del contenedor del mapa (Google Maps suele responder a wheel).
// 4) Teclado "-" con foco en el mapa.
func zoomOutMap(page playwright.Page, steps int) bool {
	if n := zoomOutClickLoop(page, []string{
		`button[aria-label="Zoom out"]`,
		`button[aria-label="Zoom Out"]`,
		`button[title="Zoom out"]`,
		`button[title="Zoom Out"]`,
		`div[role="button"][aria-label="Zoom out"]`,
		`.gm-bundled-control button[aria-label*="zoom" i]`,
		`.leaflet-control-zoom-out`,
	}, steps); n > 0 {
		return true
	}

	if n := zoomOutClickLoop(page, []string{
		`a:text-is("Zoom Out")`,
		`td:text-is("Zoom Out")`,
		`span:text-is("Zoom Out")`,
	}, steps); n > 0 {
		return true
	}

	for _, role := range []struct {
		r    playwright.AriaRole
		name string
	}{
		{playwright.AriaRole("link"), "Zoom Out"},
		{playwright.AriaRole("button"), "Zoom Out"},
	} {
		loc := page.GetByRole(role.r, playwright.PageGetByRoleOptions{
			Name:  role.name,
			Exact: playwright.Bool(true),
		}).First()
		if c, _ := loc.Count(); c == 0 {
			continue
		}
		if n := zoomOutClickOnLocator(page, loc, steps); n > 0 {
			return true
		}
	}

	if zoomOutViaMouseWheel(page, steps) {
		return true
	}

	mapEl := page.Locator("#map, #map_canvas, .map-canvas, canvas").First()
	if c, _ := mapEl.Count(); c > 0 {
		_ = mapEl.Click(playwright.LocatorClickOptions{
			Timeout: playwright.Float(1500),
			Force:   playwright.Bool(true),
		})
		for z := 0; z < steps; z++ {
			if err := page.Keyboard().Press("Minus"); err != nil {
				return z > 0
			}
			page.WaitForTimeout(150)
		}
		return true
	}

	return false
}

func zoomOutClickLoop(page playwright.Page, selectors []string, steps int) int {
	for _, sel := range selectors {
		btn := page.Locator(sel).First()
		if n, _ := btn.Count(); n == 0 {
			continue
		}
		if err := btn.WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(2500),
		}); err != nil {
			continue
		}
		if k := zoomOutClickOnLocator(page, btn, steps); k > 0 {
			return k
		}
	}
	return 0
}

func zoomOutClickOnLocator(page playwright.Page, btn playwright.Locator, steps int) int {
	clicked := 0
	for z := 0; z < steps; z++ {
		if err := btn.Click(playwright.LocatorClickOptions{
			Timeout: playwright.Float(2000),
			Force:   playwright.Bool(true),
		}); err != nil {
			break
		}
		clicked++
		page.WaitForTimeout(180)
	}
	return clicked
}

// zoomOutViaMouseWheel mueve el cursor al centro del mapa y envía wheel hacia abajo (típico zoom out en Google Maps).
func zoomOutViaMouseWheel(page playwright.Page, steps int) bool {
	mapLoc := page.Locator("#map_canvas, #map, .map-canvas").First()
	if n, _ := mapLoc.Count(); n == 0 {
		return false
	}
	box, err := mapLoc.BoundingBox()
	if err != nil || box == nil || box.Width < 50 || box.Height < 50 {
		return false
	}
	cx := box.X + box.Width/2
	cy := box.Y + box.Height/2
	mouse := page.Mouse()
	if err := mouse.Move(cx, cy); err != nil {
		return false
	}
	for z := 0; z < steps; z++ {
		if err := mouse.Wheel(0, 700); err != nil {
			return z > 0
		}
		page.WaitForTimeout(200)
	}
	return true
}

// setBeamwidthFilterFixed escribe el campo Beamwidth Filter con un valor fijo (p. ej. "90").
func setBeamwidthFilterFixed(page playwright.Page, grados string) {
	grados = strings.TrimSpace(grados)
	if grados == "" {
		return
	}
	for _, sel := range []string{
		"#BeamwidthFilter",
		"#BeamWidthFilter",
		"#MainContent_BeamwidthFilter",
		"#MainContent_BeamWidthFilter",
		`input[id*="Beamwidth"]`,
		`input[id*="beamwidth"]`,
	} {
		loc := page.Locator(sel).First()
		n, _ := loc.Count()
		if n == 0 {
			continue
		}
		_ = commitInputUntilValue(page, sel, grados, 5*time.Second)
		return
	}
}

// dispatchInputChange fuerza value + eventos input/change (Angular/KO suelen ignorar solo Fill).
func dispatchInputChange(page playwright.Page, sel, val string) {
	_, _ = page.Evaluate(fmt.Sprintf(`() => {
		const el = document.querySelector(%q);
		if (!el) return;
		el.focus();
		el.value = %q;
		el.dispatchEvent(new Event("input", { bubbles: true }));
		el.dispatchEvent(new Event("change", { bubbles: true }));
	}`, sel, val))
}

func inputStringsMatchDegrees(got, want string) bool {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if got == want {
		return true
	}
	gf, err1 := strconv.ParseFloat(got, 64)
	wf, err2 := strconv.ParseFloat(want, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	d := gf - wf
	if d < 0 {
		d = -d
	}
	return d < 0.02
}

// commitInputUntilValue rellena y reintenta hasta que el valor en DOM coincide (evita race tras Search / RadioSystem).
func commitInputUntilValue(page playwright.Page, selector, want string, maxWait time.Duration) error {
	want = strings.TrimSpace(want)
	if want == "" {
		return nil
	}
	loc := page.Locator(selector).First()
	if err := loc.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(8000),
	}); err != nil {
		return fmt.Errorf("campo %s no visible: %w", selector, err)
	}

	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		_ = loc.Click(playwright.LocatorClickOptions{
			Timeout: playwright.Float(2000),
			Force:   playwright.Bool(true),
		})
		_ = loc.Fill(want)
		_ = loc.Blur()
		dispatchInputChange(page, selector, want)

		got, err := loc.InputValue()
		if err == nil && inputStringsMatchDegrees(got, want) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	got, _ := loc.InputValue()
	return fmt.Errorf("timeout leyendo %s (último valor %q, esperado %q)", selector, got, want)
}

func inputValueMatches(page playwright.Page, selector, want string) (bool, string) {
	loc := page.Locator(selector).First()
	got, err := loc.InputValue()
	if err != nil {
		return false, ""
	}
	return inputStringsMatchDegrees(got, want), strings.TrimSpace(got)
}

// ensureAzimuthCommitted evita #showFilter hasta que #Azimuth en el DOM coincide con want (hasta ~40s).
// El sitio suele resetear el azimut a 0 tras beamwidth u otros handlers; por eso se reintenta tras networkidle.
func ensureAzimuthCommitted(page playwright.Page, workerID int, apName, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	deadline := time.Now().Add(40 * time.Second)
	for attempt := 1; time.Now().Before(deadline); attempt++ {
		remaining := time.Until(deadline)
		perAttempt := 7 * time.Second
		if remaining < perAttempt {
			perAttempt = remaining
		}
		if perAttempt < 800*time.Millisecond {
			break
		}
		azi := page.Locator("#Azimuth").First()
		_ = azi.ScrollIntoViewIfNeeded()
		_ = commitInputUntilValue(page, "#Azimuth", want, perAttempt)
		ok, got := inputValueMatches(page, "#Azimuth", want)
		if ok {
			if attempt > 1 {
				log.Printf("[Worker-%d] azimut OK para %s tras %d intentos", workerID, apName, attempt)
			}
			return true
		}
		log.Printf("[Worker-%d] azimut intento %d %s: DOM=%q esperado=%q", workerID, attempt, apName, got, want)
		_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   playwright.LoadStateNetworkidle,
			Timeout: playwright.Float(6500),
		})
		page.WaitForTimeout(400)
	}
	return false
}

// Close limpia los recursos al terminar la aplicación
func (s *TowerScraper) Close() {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if s.context != nil {
		s.context.Close()
	}
	if s.browser != nil {
		s.browser.Close()
	}
	if s.pw != nil {
		s.pw.Stop()
	}
	if s.redis != nil {
		_ = s.redis.Close()
	}
	if s.coverageRedis != nil {
		_ = s.coverageRedis.Close()
	}
}
