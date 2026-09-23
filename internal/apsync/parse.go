package apsync

import (
	"regexp"
	"strings"
)

// ParsedCoverage es el name de GetCoverageList partido en sitio / AP / tipo.
type ParsedCoverage struct {
	Sitio string
	AP    string
	Tipo  string
}

// apExtractors: el AP va al final de "sitio-AP" (sin espacios). Orden: más específico primero.
// El sitio puede tener espacios (COLLORES TOWER) o guiones (LA-TABLA).
var apExtractors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(.+)-((?:OSNAP|OSANP)\S+)\s+(.+)$`),
	regexp.MustCompile(`(?i)^(.+)-([A-Z0-9]*SEC-\d+)\s+(.+)$`),
	regexp.MustCompile(`(?i)^(.+)-(\d+[A-Z]+-\d+(?:-[A-Z0-9]+)?)\s+(.+)$`),
	regexp.MustCompile(`(?i)^(.+)-([A-Z]+\d+-[A-Z0-9]+)\s+(.+)$`),
	regexp.MustCompile(`(?i)^(.+)-([A-Z]+-\d+)\s+(.+)$`),
	regexp.MustCompile(`(?i)^(.+)-([A-Z]+\d+)\s+(.+)$`),
}

// ParseCoverageName extrae sitio, AP y tipo desde un name OSN-<sitio>-<AP> <tipo>.
// Solo acepta names que empiecen por OSN- (case insensitive).
func ParseCoverageName(name string) (ParsedCoverage, bool) {
	name = strings.TrimSpace(name)
	if name == "" || !hasOSNPrefix(name) {
		return ParsedCoverage{}, false
	}
	rest := name[4:]
	for _, re := range apExtractors {
		m := re.FindStringSubmatch(rest)
		if m == nil {
			continue
		}
		sitio := strings.TrimSpace(m[1])
		ap := strings.TrimSpace(m[2])
		tipo := strings.TrimSpace(m[3])
		if sitio == "" || ap == "" || tipo == "" {
			continue
		}
		return ParsedCoverage{Sitio: sitio, AP: ap, Tipo: tipo}, true
	}
	return ParsedCoverage{}, false
}

func hasOSNPrefix(name string) bool {
	if len(name) < 4 {
		return false
	}
	return strings.EqualFold(name[:4], "OSN-")
}

// TipoConOMNI añade OMNI al tipo si beamwidthFilter es 360 o 0 y aún no lo lleva.
func TipoConOMNI(tipo, beamwidthFilter string) string {
	tipo = strings.TrimSpace(tipo)
	if !isOmniBeamwidth(beamwidthFilter) {
		return tipo
	}
	if strings.Contains(strings.ToLower(tipo), "omni") {
		return tipo
	}
	if tipo == "" {
		return "OMNI"
	}
	return tipo + " OMNI"
}

func isOmniBeamwidth(raw string) bool {
	switch strings.TrimSpace(raw) {
	case "360", "0", "360.0", "0.0":
		return true
	default:
		return false
	}
}

// TorreNombreDesdeSite usa el name de GetSiteList (vía towersiteID) y, si falta, el sitio parseado.
// Se guarda sin prefijo OSN. / OSN- para casar con ObtenerAPsPorTorre.
func TorreNombreDesdeSite(siteName, parsedSitio string) string {
	name := strings.TrimSpace(siteName)
	if name == "" {
		name = strings.TrimSpace(parsedSitio)
	}
	name = stripOSNPrefix(name)
	return strings.TrimSpace(name)
}

func stripOSNPrefix(name string) string {
	name = strings.TrimSpace(name)
	for _, p := range []string{"OSN.", "OSN-", "osn.", "osn-"} {
		if strings.HasPrefix(name, p) {
			return strings.TrimSpace(name[len(p):])
		}
	}
	if len(name) >= 4 && strings.EqualFold(name[:4], "OSN.") {
		return strings.TrimSpace(name[4:])
	}
	if len(name) >= 4 && strings.EqualFold(name[:4], "OSN-") {
		return strings.TrimSpace(name[4:])
	}
	return name
}
