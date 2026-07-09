package geo

import "math"

const radioTierraKm = 6371.0
const kmPerMile = 1.609344

// KmToMiles convierte kilómetros a millas.
func KmToMiles(km float64) float64 {
	return math.Round((km/kmPerMile)*100) / 100
}

// gradosARadianes convierte grados a radianes
func gradosARadianes(grados float64) float64 {
	return grados * math.Pi / 180
}

// CalcularDistancia devuelve la distancia en kilómetros entre dos coordenadas
func CalcularDistancia(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := gradosARadianes(lat2 - lat1)
	dLon := gradosARadianes(lon2 - lon1)

	lat1Rad := gradosARadianes(lat1)
	lat2Rad := gradosARadianes(lat2)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(lat1Rad)*math.Cos(lat2Rad)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return math.Round((radioTierraKm*c)*100) / 100 // Redondeado a 2 decimales
}

// CalcularAngulo retorna el ángulo (bearing) en grados desde el punto 1 (Torre) al punto 2 (Cliente)
// 0° es el Norte, 90° es el Este, etc.
func CalcularAngulo(lat1, lon1, lat2, lon2 float64) float64 {
	// Convertir grados a radianes
	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180

	dLon := lon2Rad - lon1Rad

	// Fórmula de rumbo inicial (Forward azimuth)
	X := math.Sin(dLon) * math.Cos(lat2Rad)
	Y := math.Cos(lat1Rad)*math.Sin(lat2Rad) - math.Sin(lat1Rad)*math.Cos(lat2Rad)*math.Cos(dLon)

	brng := math.Atan2(X, Y)
	brngDeg := brng * 180 / math.Pi

	// Normalizar el ángulo para que siempre esté entre 0 y 360 grados
	if brngDeg < 0 {
		brngDeg += 360
	}
	return brngDeg
}

// EstaEnCobertura evalúa si el cliente está dentro del cono de la antena
func EstaEnCobertura(azimutAP, bearingCliente, beamwidth float64) bool {
	if beamwidth >= 360 {
		return true
	}

	// Diferencia absoluta entre hacia donde mira la antena y donde está el cliente
	diff := math.Abs(azimutAP - bearingCliente)

	// Si la diferencia cruza el meridiano 360°/0° (ej: azimut 350, bearing 10 -> diff 340, debería ser 20)
	if diff > 180 {
		diff = 360 - diff
	}

	// Para un beamwidth de 90, la antena cubre 45 grados a su izquierda y 45 a su derecha
	margen := beamwidth / 2.0

	return diff <= margen
}
