package models

// SNMPStatusResponse resultado de una consulta SNMP directa a un AP.
type SNMPStatusResponse struct {
	APName             string `json:"ap_name,omitempty"`
	TorreNombre        string `json:"torre_nombre,omitempty"`
	Tipo               string `json:"tipo_de_antena"`
	IPAddress          string `json:"ip_address"`
	ClientesConectados int    `json:"clientes_conectados"`
	EstaSaturado       *bool  `json:"esta_saturado,omitempty"`
	EstadoCapacidad    string `json:"estado_capacidad,omitempty"`
}
