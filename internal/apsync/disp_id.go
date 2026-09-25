package apsync

import (
	"strings"

	"tower-scraper/internal/db"
)

// dispositivoNameToken toma el AP al inicio de dispositivos.dispositivo.
// "OSNAP16-A ePMP3000" y "OSNAP16-A (Rocket AC)" → "OSNAP16-A".
func dispositivoNameToken(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if i := strings.IndexAny(name, " \t("); i >= 0 {
		name = name[:i]
	}
	return strings.ToUpper(strings.TrimSpace(name))
}

func dispositivoLookupKey(name, ip string) string {
	token := dispositivoNameToken(name)
	ip = strings.TrimSpace(ip)
	if token == "" || ip == "" {
		return ""
	}
	return token + "\x00" + ip
}

// indexDispositivosByNameIP indexa id de dispositivos por (token de nombre + ip).
// Si hay duplicados con la misma clave, se queda el id más bajo.
func indexDispositivosByNameIP(rows []db.DispositivoCatalogo) map[string]int {
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		key := dispositivoLookupKey(r.Dispositivo, r.IPAddress)
		if key == "" {
			continue
		}
		id := int(r.ID)
		if prev, ok := out[key]; !ok || id < prev {
			out[key] = id
		}
	}
	return out
}

func lookupDispID(index map[string]int, apName, ip string) (int, bool) {
	key := dispositivoLookupKey(apName, ip)
	if key == "" {
		return 0, false
	}
	id, ok := index[key]
	return id, ok
}
