package snmp

import (
	"fmt"
	"os"
	"strings"
	"time"
	"tower-scraper/internal/models"

	"github.com/gosnmp/gosnmp"
)

// CheckSaturation consulta por SNMP si un AP está saturado
func CheckSaturation(ap models.AccessPoint) (models.APStatus, error) {
	// 1. Defensa contra datos sucios en la IP (NULL o cadena vacía)
	ipUpper := strings.ToUpper(strings.TrimSpace(ap.IPAddress))
	if ipUpper == "" || ipUpper == "NULL" {
		// Retornamos sin error, pero con el estado claro para el JSON
		return models.APStatus{
			APName:      ap.APName,
			Type:        ap.Tipo,
			IsSaturated: boolPtr(false),
			Message:     "Sin IP configurada en DB",
		}, nil
	}

	// 2. Normalización agresiva del tipo de antena
	t := strings.ToLower(ap.Tipo)
	var oid string
	var useWaveWalk bool

	// Lógica inclusiva: Atrapa (Rocket AC), Lite AC, ePMP3000 (OMNI), etc.
	if strings.Contains(t, "epmp") || strings.Contains(t, "cambium") {
		oid = oidCambium
	} else if isWaveAPType(t) {
		// Wave AP (sysName suele contener "WaveAP"): conteo por tabla de estaciones, no el escalar AirMAX.
		useWaveWalk = true
	} else if strings.Contains(t, "rocket") ||
		strings.Contains(t, "lite") ||
		strings.Contains(t, "ac") ||
		strings.Contains(t, "ubiquiti") {
		oid = oidUbiquiti
	} else {
		// Atrapa casos como "DESCONOCIDO" u otros modelos no mapeados
		return models.APStatus{
			APName:      ap.APName,
			Type:        ap.Tipo,
			IsSaturated: boolPtr(false),
			Message:     "Tipo de equipo sin soporte SNMP mapeado",
		}, nil
	}

	// 3. Configurar el cliente SNMP
	community := os.Getenv("SNMP_COMMUNITY")
	if community == "" {
		community = "osnsnmpro" // Tu fallback por defecto
	}

	// Ubiquiti AirMAX AC (Rocket AC, LiteBeam, etc.): la red confirma SNMP v1 en el escalar de clientes.
	// Cambium / Wave siguen en v2c (GET-BULK / tabla Wave).
	snmpVersion := gosnmp.Version2c
	if !useWaveWalk && oid == oidUbiquiti {
		snmpVersion = gosnmp.Version1
	}

	snmpClient := &gosnmp.GoSNMP{
		Target:    ap.IPAddress,
		Port:      161,
		Community: community,
		Version:   snmpVersion,
		Timeout:   time.Duration(2) * time.Second,
		Retries:   2,
	}

	err := snmpClient.Connect()
	if err != nil {
		// Error de red (timeout, antena apagada, firewall). Lo reportamos en el JSON en lugar de fallar el worker.
		return models.APStatus{
			APName:      ap.APName,
			Type:        ap.Tipo,
			IsSaturated: boolPtr(false),
			Message:     fmt.Sprintf("Inalcanzable por red: %v", err),
		}, nil
	}
	defer snmpClient.Conn.Close()

	// 4. Conteo de clientes (escalar o walk según familia)
	var clientesConectados int
	if useWaveWalk {
		n, werr := waveStationClientCount(snmpClient)
		if werr != nil {
			return models.APStatus{
				APName:      ap.APName,
				Type:        ap.Tipo,
				IsSaturated: boolPtr(false),
				Message:     werr.Error(),
			}, nil
		}
		clientesConectados = n
	} else {
		result, err := snmpClient.Get([]string{oid})
		if err != nil {
			return models.APStatus{
				APName:      ap.APName,
				Type:        ap.Tipo,
				IsSaturated: boolPtr(false),
				Message:     fmt.Sprintf("Error en consulta OID: %v", err),
			}, nil
		}
		if len(result.Variables) > 0 {
			clientesConectados = intFromSNMPValue(result.Variables[0].Value)
		}
	}

	// 5. Evaluar la saturación
	return EvaluateAP(ap.Tipo, clientesConectados), nil
}

func isWaveAPType(t string) bool {
	return strings.Contains(t, "wave") || strings.Contains(t, "wabe") // "wabe" = typo frecuente de Wave
}

// waveStationClientCount cuenta estaciones en la tabla Wave (UI-AF60). Si el walk falla, intenta el escalar AirMAX.
func waveStationClientCount(c *gosnmp.GoSNMP) (int, error) {
	pdus, walkErr := c.BulkWalkAll(oidWaveAPStaMac)
	if walkErr == nil {
		return len(pdus), nil
	}
	n, scalarErr := scalarStaCount(c, oidUbiquiti)
	if scalarErr != nil {
		return 0, fmt.Errorf("Wave AP: tabla estaciones (%v); respaldo OID AirMAX (%v)", walkErr, scalarErr)
	}
	return n, nil
}

func scalarStaCount(c *gosnmp.GoSNMP, oid string) (int, error) {
	result, err := c.Get([]string{oid})
	if err != nil {
		return 0, err
	}
	if len(result.Variables) == 0 {
		return 0, fmt.Errorf("respuesta SNMP vacía para %s", oid)
	}
	return intFromSNMPValue(result.Variables[0].Value), nil
}

func intFromSNMPValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	default:
		return 0
	}
}
