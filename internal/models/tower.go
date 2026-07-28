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
	Torre              DatosTorre `json:"-"`
	Antena             string     `json:"antena"`
	Tipo               string     `json:"tipo_de_antena"`
	Distancia          float64    `json:"distancia_entre_antena_y_cliente_km"`
	Cobertura          bool       `json:"cliente_con_cobertura"`
	NombreTorre        string     `json:"nombre_torre"`
	ClientesConectados *int       `json:"clientes_conectados,omitempty"`
	// SNMP / capacidad: esta_saturado solo cuando hubo lectura OID y regla de umbral (EvaluateAP).
	EstaSaturado    *bool  `json:"esta_saturado,omitempty"`
	EstadoCapacidad string `json:"estado_capacidad,omitempty"`
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
