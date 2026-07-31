package models

import (
	"encoding/json"
	"testing"
)

func TestToAPIIncludesSiteTilt(t *testing.T) {
	torre := TowerCoverage{
		TowerName:       "OSN.A",
		Latitude:        "18.1",
		Longitude:       "-66.2",
		Group:           "Fiber",
		Elevation:       "50m",
		TowerHeight:     "18m",
		Alignment:       "64°",
		Tilt:            "-0.07°",
		ClientHeight:    "6",
		Status:          "Good Link",
		Signal:          "-49",
		DistanceKm:      "2.9km",
		Distance:        "1.8mi",
		SuggestedHeight: "0m",
		PathImage:       "img",
	}
	r := RespuestaMCP{
		Antena:      "AP1",
		Tipo:        "ubiquiti",
		Distancia:   1.2,
		Cobertura:   true,
		NombreTorre: "OSN.A",
	}
	r.ApplySiteFields(torre)

	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"antena":"AP1","tipo_de_antena":"ubiquiti","distancia_entre_antena_y_cliente_km":1.2,"cliente_con_cobertura":true,"nombre_torre":"OSN.A"}` {
		t.Fatalf("MCP no debe exponer campos de sitio: %s", raw)
	}

	api := r.ToAPI()
	if api.Client.SiteTilt != "-0.07°" {
		t.Fatalf("site_tilt: %q", api.Client.SiteTilt)
	}
	if api.Group != "Fiber" || api.PathImage != "img" {
		t.Fatalf("group/path_image: %+v", api)
	}
	if api.Tower.Location != "18.1, -66.2" || api.Performance.Status != "Good Link" {
		t.Fatalf("tower/performance: %+v", api)
	}

	apiRaw, err := json.Marshal(api)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(apiRaw, &m); err != nil {
		t.Fatal(err)
	}
	client, ok := m["client"].(map[string]any)
	if !ok {
		t.Fatalf("client ausente: %s", apiRaw)
	}
	if _, hasTilt := client["tilt"]; hasTilt {
		t.Fatalf("no debe existir tilt, solo site_tilt: %v", client)
	}
	if client["site_tilt"] != "-0.07°" {
		t.Fatalf("site_tilt JSON: %v", client["site_tilt"])
	}
}
