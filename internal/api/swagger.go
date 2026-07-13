package api

import (
	"log"
	"net/http"

	"tower-scraper/internal/api/docassets"
)

// RegisterSwagger expone la especificación OpenAPI y la UI de Swagger.
func RegisterSwagger() {
	http.HandleFunc("/openapi.yaml", serveOpenAPI)
	http.HandleFunc("/swagger", redirectSwagger)
	http.HandleFunc("/swagger/", serveSwaggerUI)
}

func serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
		return
	}
	spec, err := docassets.OpenAPISpec()
	if err != nil {
		log.Printf("error leyendo openapi embebido: %v", err)
		http.Error(w, "especificación no disponible", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write(spec)
}

func redirectSwagger(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/swagger/", http.StatusMovedPermanently)
}

func serveSwaggerUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(docassets.SwaggerHTML))
}
