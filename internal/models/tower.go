package models

// TowerCoverage representa los datos que extraeremos de cada torre
type TowerCoverage struct {
	TowerName string
	Latitude  string
	Longitude string
	Alignment string
	Tilt      string
	Distance  string
	Signal    string
	Status    string
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
