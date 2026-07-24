package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"tower-scraper/internal/api"
	"tower-scraper/internal/config"
	"tower-scraper/internal/coverage"
	"tower-scraper/internal/db"
	"tower-scraper/internal/mcpimage"
	"tower-scraper/internal/rabbitmq"
	"tower-scraper/internal/scraper"
)

func main() {
	log.Println("Cargando configuración...")
	cfg := config.LoadConfig()

	// INICIALIZAMOS LA BASE DE DATOS SOLO PARA LECTURA DE APs
	log.Println("Conectando a la base de datos MySQL...")
	dbClient, err := db.NewDBClient(cfg)
	if err != nil {
		log.Fatalf("Error inicializando DB: %v", err)
	}

	log.Println("Inicializando servicio de cobertura (API TowerCoverage)...")
	ts, err := scraper.NewTowerScraper(cfg)
	if err != nil {
		log.Fatalf("Error inicializando scraper: %v", err)
	}
	defer ts.Close()

	siteCtx, siteCancel := context.WithCancel(context.Background())
	defer siteCancel()
	if err := ts.APIClient().EnsureSiteListCache(siteCtx); err != nil {
		log.Fatalf("Error preparando cache GetSiteList en Redis: %v", err)
	}
	ts.APIClient().StartSundaySiteListRefresh(siteCtx)

	var rateGate coverage.RateGate
	if cfg.RabbitMQURL != "" {
		rl, err := rabbitmq.NewRateLimiter(rabbitmq.Options{
			URL:    cfg.RabbitMQURL,
			Queue:  cfg.RabbitMQRateQueue,
			RPS:    cfg.CoverageMaxRPS,
			Burst:  cfg.CoverageRateBurst,
			Leader: cfg.RabbitMQRateLeader,
		})
		if err != nil {
			log.Fatalf("Error conectando RabbitMQ (rate limit): %v", err)
		}
		defer rl.Close()
		rateGate = rl
		if !cfg.RabbitMQRateLeader {
			log.Println("ℹ️ RABBITMQ_RATE_LEADER=false: esta instancia solo consume tokens. Una de las dos debe ser leader.")
		}
	} else {
		log.Println("⚠️ RABBITMQ_URL no definida: sin límite global de peticiones entre instancias")
	}

	mcpServer := server.NewMCPServer("TowerCoverageService", "1.0.0")

	tool := mcp.NewTool("get_tower_coverage",
		mcp.WithDescription("Obtiene torres cercanas y verifica cobertura de APs (trigonometría y SNMP). "+
			"Un punto: lat + lon. Varios puntos: rellena locations_json con un array JSON (string). "+
			"Varias consultas en paralelo. REST para n8n: POST /api/coverage/full con el mismo cuerpo. "+
			"Si ya se calculó una cobertura full dentro de ~25 m (COVERAGE_CACHE_RADIUS_M), reutiliza ese resultado."),
		mcp.WithString("lat", mcp.Description("Latitud cliente (si es un solo punto y no usas locations_json)")),
		mcp.WithString("lon", mcp.Description("Longitud cliente (si es un solo punto y no usas locations_json)")),
		mcp.WithString("locations_json", mcp.Description(
			`Opcional. Array JSON en texto: [{"lat":"18.4","lon":"-66.2"}]. En HTTP preferir el campo "locations" como array nativo.`)),
	)

	mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = ctx // cancelación del cliente MCP (futuro)
		coords, err := coordsFromArgsMap(request.GetArguments())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if len(coords) == 1 {
			log.Printf("🤖 MCP Request -> Lat: %s, Lon: %s", coords[0].lat, coords[0].lon)
		} else {
			log.Printf("🤖 MCP Request -> %d ubicaciones en paralelo", len(coords))
		}

		resultJSON, err := coverage.RunConsultas(ts, dbClient, toCoverageCoords(coords), rateGate)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Fallo consulta cobertura: %v", err)), nil
		}

		if len(coords) == 1 {
			log.Println("✅ Respuesta MCP enviada al agente (1 ubicación).")
		} else {
			log.Printf("✅ Respuesta MCP enviada al agente (%d ubicaciones).", len(coords))
		}
		return mcp.NewToolResultText(string(resultJSON)), nil
	})

	mapsTool := mcp.NewTool("get_google_maps_screenshot",
		mcp.WithDescription("Abre Google Maps en las coordenadas indicadas y devuelve una captura PNG. "+
			"La respuesta incluye contenido tipo image (MCP); en n8n activa Convert to Binary en el nodo MCP Client."),
		mcp.WithString("lat", mcp.Required(), mcp.Description("Latitud del punto a mostrar")),
		mcp.WithString("lon", mcp.Required(), mcp.Description("Longitud del punto a mostrar")),
	)

	mcpServer.AddTool(mapsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = ctx
		args := request.GetArguments()
		lat, latOk := normalizeCoordArg(args["lat"])
		lon, lonOk := normalizeCoordArg(args["lon"])
		if !latOk || !lonOk {
			return mcp.NewToolResultError("indica lat y lon"), nil
		}

		log.Printf("🗺️ MCP Google Maps -> Lat: %s, Lon: %s", lat, lon)

		png, err := ts.ScreenshotGoogleMaps(lat, lon)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fallo captura Google Maps: %v", err)), nil
		}

		caption := fmt.Sprintf("Google Maps en %s, %s", lat, lon)
		log.Println("✅ Captura Google Maps enviada al agente.")
		return mcpimage.PNGToolResult(caption, png), nil
	})

	// 4. Configuración del Transporte (Dual Mode: Stdio o SSE)
	mcpTransport := os.Getenv("MCP_TRANSPORT")
	if mcpTransport == "" {
		mcpTransport = "stdio"
	}

	if mcpTransport == "sse" {
		log.Printf("🚀 Servidor iniciado en modo SSE (Remoto). Escuchando en el puerto %s...", cfg.AppPort)

		if cfg.MCPAPIKey == "" {
			log.Println("⚠️ MCP_API_KEY no definida: los endpoints /sse y /message aceptan conexiones sin autenticación")
		}
		if cfg.JWTSecret == "" {
			log.Println("⚠️ JWT_SECRET no definida: los endpoints /api/* aceptan peticiones sin autenticación")
		}

		sseServer := server.NewSSEServer(mcpServer)

		http.Handle("/sse", mcpBearerAuth(cfg.MCPAPIKey, sseServer.SSEHandler()))
		http.Handle("/message", mcpBearerAuth(cfg.MCPAPIKey, sseServer.MessageHandler()))

		api.Register(&api.Handler{
			Scraper:  ts,
			DB:       dbClient,
			RateGate: rateGate,
			ParseCoords: func(raw any) ([]coverage.Coord, error) {
				pairs, err := coordsFromAnyRoot(raw)
				if err != nil {
					return nil, err
				}
				return toCoverageCoords(pairs), nil
			},
		}, cfg.JWTSecret, cfg.SwaggerEnabled)
		if cfg.SwaggerEnabled {
			log.Printf("📖 Documentación Swagger: http://localhost:%s/swagger/", cfg.AppPort)
		} else {
			log.Println("📖 Swagger deshabilitado (SWAGGER_ENABLED=false)")
		}

		addr := fmt.Sprintf(":%s", cfg.AppPort)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatalf("Error crítico en el servidor HTTP/SSE: %v", err)
		}

	} else {
		log.Println("🚀 Servidor MCP iniciado en modo Stdio (Local). Esperando instrucciones...")
		if err := server.ServeStdio(mcpServer); err != nil {
			log.Fatalf("Error crítico en el servidor MCP por Stdio: %v", err)
		}
	}
}

func mcpBearerAuth(apiKey string, next http.Handler) http.Handler {
	if apiKey == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !bearerTokenMatches(r.Header.Get("Authorization"), apiKey) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="MCP", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerTokenMatches(authHeader, expected string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return false
	}
	token := strings.TrimSpace(authHeader[len(prefix):])
	if token == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

type coordPair struct {
	lat, lon string
}

func normalizeCoordArg(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		return s, s != ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case json.Number:
		s := strings.TrimSpace(x.String())
		return s, s != ""
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	default:
		return "", false
	}
}

// coordsFromAnyRoot acepta cuerpo objeto o array raíz (n8n / clientes variados).
func coordsFromAnyRoot(raw any) ([]coordPair, error) {
	switch v := raw.(type) {
	case map[string]any:
		return coordsFromArgsMap(v)
	case []any:
		return extractPairsFromSlice(v)
	default:
		return nil, fmt.Errorf("JSON: usa objeto {\"lat\",\"lon\" o \"locations\"...} o array [{\"lat\",\"lon\"},...]")
	}
}

// coordsFromArgsMap interpreta herramienta MCP y POST /api/coverage/full.
// Prioridad: locations_json > locations > lat + lon.
func coordsFromArgsMap(args map[string]any) ([]coordPair, error) {
	if args == nil {
		return nil, fmt.Errorf("sin argumentos")
	}

	if raw, ok := args["locations_json"]; ok && raw != nil {
		s := strings.TrimSpace(coerceToString(raw))
		if s != "" {
			pairs, err := parseLocationsJSONArrayBytes([]byte(s))
			if err != nil {
				return nil, fmt.Errorf("locations_json: %w", err)
			}
			return pairs, nil
		}
	}

	if raw, ok := args["locations"]; ok && raw != nil {
		pairs, err := parseLocationsField(raw)
		if err != nil {
			return nil, fmt.Errorf("locations: %w", err)
		}
		return pairs, nil
	}

	lat, latOk := normalizeCoordArg(args["lat"])
	lon, lonOk := normalizeCoordArg(args["lon"])
	if !latOk || !lonOk {
		return nil, fmt.Errorf("indica lat y lon, o locations (array o string JSON), o locations_json (texto con array JSON)")
	}
	return []coordPair{{lat: lat, lon: lon}}, nil
}

func mapsToCoordPairs(arr []map[string]any) []coordPair {
	var pairs []coordPair
	for _, m := range arr {
		lat, latOk := normalizeCoordArg(m["lat"])
		lon, lonOk := normalizeCoordArg(m["lon"])
		if latOk && lonOk {
			pairs = append(pairs, coordPair{lat: lat, lon: lon})
		}
	}
	return pairs
}

func extractPairsFromSlice(arr []any) ([]coordPair, error) {
	var ms []map[string]any
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ms = append(ms, m)
	}
	pairs := mapsToCoordPairs(ms)
	if len(pairs) == 0 {
		return nil, fmt.Errorf("array sin objetos {{lat, lon}} válidos")
	}
	return pairs, nil
}

// parseLocationsField acepta array nativo, string JSON, u objeto único {lat,lon}.
func parseLocationsField(raw any) ([]coordPair, error) {
	switch v := raw.(type) {
	case []any:
		return extractPairsFromSlice(v)
	case string:
		return parseLocationsJSONArrayBytes([]byte(strings.TrimSpace(v)))
	case map[string]any:
		return extractPairsFromSlice([]any{v})
	default:
		return nil, fmt.Errorf("tipo no soportado (%T); usa array o string con JSON array", raw)
	}
}

// peelJSONStringWrappers quita comillas JSON externas repetidas ("\"[{...}]\"" → contenido interno).
func peelJSONStringWrappers(b []byte) []byte {
	b = bytes.TrimSpace(b)
	for i := 0; i < 6; i++ {
		var inner string
		if err := json.Unmarshal(b, &inner); err != nil {
			break
		}
		next := []byte(strings.TrimSpace(inner))
		if len(next) == 0 || bytes.Equal(b, next) {
			break
		}
		b = next
	}
	return b
}

func tryUnmarshalLocationArray(b []byte) ([]coordPair, bool) {
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil || len(arr) == 0 {
		return nil, false
	}
	p := mapsToCoordPairs(arr)
	if len(p) == 0 {
		return nil, false
	}
	return p, true
}

func tryUnmarshalSingleLocationObject(b []byte) ([]coordPair, bool) {
	var one map[string]any
	if err := json.Unmarshal(b, &one); err != nil {
		return nil, false
	}
	lat, ok1 := normalizeCoordArg(one["lat"])
	lon, ok2 := normalizeCoordArg(one["lon"])
	if !ok1 || !ok2 {
		return nil, false
	}
	return []coordPair{{lat: lat, lon: lon}}, true
}

// parseLocationsJSONArrayBytes tolera string JSON anidado y comillas escapadas típicas de n8n (\" → ").
func parseLocationsJSONArrayBytes(b []byte) ([]coordPair, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, fmt.Errorf("vacío")
	}

	candidates := make([][]byte, 0, 4)
	add := func(x []byte) {
		x = bytes.TrimSpace(x)
		if len(x) == 0 {
			return
		}
		for _, existing := range candidates {
			if bytes.Equal(existing, x) {
				return
			}
		}
		candidates = append(candidates, x)
	}

	add(b)
	add(peelJSONStringWrappers(b))

	sb := string(peelJSONStringWrappers(b))
	if strings.Contains(sb, `\"`) {
		add([]byte(strings.ReplaceAll(sb, `\"`, `"`)))
	}

	for _, cand := range candidates {
		if pairs, ok := tryUnmarshalLocationArray(cand); ok {
			return pairs, nil
		}
	}
	for _, cand := range candidates {
		if pairs, ok := tryUnmarshalSingleLocationObject(cand); ok {
			return pairs, nil
		}
	}

	return nil, fmt.Errorf("no es un array [{lat,lon},...] ni un objeto {{lat,lon}}; revisa comillas y escapes")
}

func coerceToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

func toCoverageCoords(pairs []coordPair) []coverage.Coord {
	out := make([]coverage.Coord, len(pairs))
	for i, c := range pairs {
		out[i] = coverage.Coord{Lat: c.lat, Lon: c.lon}
	}
	return out
}
