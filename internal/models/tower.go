package models

import "strings"

// TowerCoverage representa los datos internos de una torre con enlace viable.
type TowerCoverage struct {
	TowerName string
	Latitude  string
	Longitude string
	Alignment string
	Tilt      string
	Distance  string
	Signal    string
	Status    string

	// Campos LinkPath para armar la respuesta de POST /api/coverage.
	Group           string
	Elevation       string
	TowerHeight     string
	ClientHeight    string
	SuggestedHeight string
	DistanceKm      string
	PathImage       string
}

// CoverageLightItem es la respuesta de POST /api/coverage, organizada como el
// resumen visual de Link Path (torre / cliente / rendimiento + imagen).
type CoverageLightItem struct {
	TowerName   string                   `json:"tower_name"`
	Group       string                   `json:"group,omitempty"`
	PathImage   string                   `json:"path_image"`
	Tower       CoverageLightTower       `json:"tower"`
	Client      CoverageLightClient      `json:"client"`
	Performance CoverageLightPerformance `json:"performance"`
}

type CoverageLightTower struct {
	Location  string `json:"location"`
	Elevation string `json:"elevation"`
	Height    string `json:"height"`
}

type CoverageLightClient struct {
	Alignment string `json:"alignment"`
	Tilt      string `json:"tilt"`
	Height    string `json:"height"`
}

type CoverageLightPerformance struct {
	Status          string `json:"status"`
	Signal          string `json:"signal"`
	Distance        string `json:"distance"`
	DistanceMi      string `json:"distance_mi"`
	SuggestedHeight string `json:"suggested_height"`
}

// ToCoverageLight convierte el modelo interno al JSON del endpoint ligero.
func (t TowerCoverage) ToCoverageLight() CoverageLightItem {
	loc := strings.TrimSpace(t.Latitude)
	if lon := strings.TrimSpace(t.Longitude); lon != "" {
		if loc != "" {
			loc = loc + ", " + lon
		} else {
			loc = lon
		}
	}
	return CoverageLightItem{
		TowerName: t.TowerName,
		Group:     t.Group,
		PathImage: t.PathImage,
		Tower: CoverageLightTower{
			Location:  loc,
			Elevation: t.Elevation,
			Height:    t.TowerHeight,
		},
		Client: CoverageLightClient{
			Alignment: t.Alignment,
			Tilt:      t.Tilt,
			Height:    t.ClientHeight,
		},
		Performance: CoverageLightPerformance{
			Status:          t.Status,
			Signal:          t.Signal,
			Distance:        t.DistanceKm,
			DistanceMi:      t.Distance,
			SuggestedHeight: t.SuggestedHeight,
		},
	}
}

type RespuestaMCP struct {
	// Torre se usa solo en procesamiento interno; no se serializa en la API/MCP.
	Torre       DatosTorre `json:"-"`
	Antena      string     `json:"antena"`
	Tipo        string     `json:"tipo_de_antena"`
	Distancia   float64    `json:"distancia_entre_antena_y_cliente_km"`
	Cobertura   bool       `json:"cliente_con_cobertura"`
	NombreTorre string     `json:"nombre_torre"`
	// PathImage solo se expone en POST /api/coverage/full (ver ToAPI); no va en MCP.
	PathImage          string `json:"-"`
	ClientesConectados *int   `json:"clientes_conectados,omitempty"`
	// SNMP / capacidad: esta_saturado solo cuando hubo lectura OID y regla de umbral (EvaluateAP).
	EstaSaturado    *bool  `json:"esta_saturado,omitempty"`
	EstadoCapacidad string `json:"estado_capacidad,omitempty"`
}

// RespuestaAPI es el JSON de cada antena en POST /api/coverage/full (incluye path_image).
type RespuestaAPI struct {
	Antena             string  `json:"antena"`
	Tipo               string  `json:"tipo_de_antena"`
	Distancia          float64 `json:"distancia_entre_antena_y_cliente_km"`
	Cobertura          bool    `json:"cliente_con_cobertura"`
	NombreTorre        string  `json:"nombre_torre"`
	PathImage          string  `json:"path_image"`
	ClientesConectados *int    `json:"clientes_conectados,omitempty"`
	EstaSaturado       *bool   `json:"esta_saturado,omitempty"`
	EstadoCapacidad    string  `json:"estado_capacidad,omitempty"`
}

// ToAPI convierte la respuesta interna al DTO REST con path_image.
func (r RespuestaMCP) ToAPI() RespuestaAPI {
	return RespuestaAPI{
		Antena:             r.Antena,
		Tipo:               r.Tipo,
		Distancia:          r.Distancia,
		Cobertura:          r.Cobertura,
		NombreTorre:        r.NombreTorre,
		PathImage:          r.PathImage,
		ClientesConectados: r.ClientesConectados,
		EstaSaturado:       r.EstaSaturado,
		EstadoCapacidad:    r.EstadoCapacidad,
	}
}

// ToAPISlice convierte un slice de respuestas internas al DTO REST.
func ToAPISlice(in []RespuestaMCP) []RespuestaAPI {
	out := make([]RespuestaAPI, len(in))
	for i := range in {
		out[i] = in[i].ToAPI()
	}
	return out
}

type DatosTorre struct {
	Align    string
	Tilt     string
	Status   string
	Latitud  float64
	Longitud float64
}

type APStatus struct {
	APName          string
	Type            string
	Clients         int
	IsSaturated     *bool
	Message         string
	EstadoCapacidad string
}

type AccessPoint struct {
	ID        int
	TowerName string
	APName    string
	Tipo      string // ej. "ubiquiti" o "cambium"
	Azimut    string
	Tilt      string
	Altura    string
	IPAddress string // NUEVO CAMPO
}
