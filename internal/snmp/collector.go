package snmp

import "tower-scraper/internal/models"

const maxClients = 25

// OIDs
const oidCambium = "1.3.6.1.4.1.17713.21.1.2.10.0"
const oidUbiquiti = "1.3.6.1.4.1.41112.1.4.5.1.15.1" // ubntWlStatStaCount (AirMAX / Rocket AC, etc.)

// oidWaveAPStaMac — UI-AF60 / Ubiquiti Wave AP: columna MAC de la tabla de estaciones; una PDU por cliente asociado.
const oidWaveAPStaMac = "1.3.6.1.4.1.41112.1.11.1.3.1.1"

func EvaluateAP(apType string, clients int) models.APStatus {
	status := models.APStatus{
		Type:    apType,
		Clients: clients,
	}

	if clients > maxClients {
		status.IsSaturated = boolPtr(true)
		status.Message = "Saturado"
	} else {
		status.IsSaturated = boolPtr(false)
		status.Message = "Con espacio"
	}
	return status
}

func boolPtr(v bool) *bool {
	return &v
}
