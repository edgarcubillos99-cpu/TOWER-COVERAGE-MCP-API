package apsync

import (
	"context"
	"log"
	"strconv"
	"strings"

	"tower-scraper/internal/db"
	"tower-scraper/internal/models"
	"tower-scraper/internal/towercoverage"
)

// Syncer cruza GetCoverageList con InterMapper (solo lectura) y recrea dispositivos_ap.
type Syncer struct {
	TC          *towercoverage.Client
	IM          *InterMapperClient
	DB          *db.DBClient
	SkipIMError bool // si InterMapper falla, sigue sin IPs en vez de abortar
}

// Resultado resume el cruce.
type Resultado struct {
	CoveragesTotales   int
	OSNProcesadas      int
	ParseFail          int
	Insertadas         int
	ConIP              int
	SinIP              int
	ConDispID          int
	InterMapperDevices int
}

// Run descarga coberturas, resuelve torre por towersiteID, cruza IPs y recrea la tabla.
func (s *Syncer) Run(ctx context.Context) (Resultado, error) {
	if err := ctx.Err(); err != nil {
		return Resultado{}, err
	}

	coverages, err := s.TC.FetchCoverageList()
	if err != nil {
		return Resultado{}, err
	}
	log.Printf("GetCoverageList: %d coberturas", len(coverages))

	sites, err := s.TC.GetSites(ctx)
	if err != nil {
		return Resultado{}, err
	}
	siteByID := make(map[string]string, len(sites))
	for _, site := range sites {
		siteByID[strconv.Itoa(site.ID)] = site.Name
	}

	var devices []Device
	if s.IM != nil && strings.TrimSpace(s.IM.BaseURL) != "" {
		devs, imErr := s.IM.FetchDevices()
		if imErr != nil {
			if !s.SkipIMError {
				return Resultado{}, imErr
			}
			log.Printf("⚠️ InterMapper no disponible (%v); se recrea la tabla sin IPs", imErr)
		} else {
			devices = devs
			log.Printf("InterMapper: %d dispositivos (solo lectura)", len(devices))
		}
	} else {
		log.Println("⚠️ INTERMAPPER_URL vacía: se recrea dispositivos_ap sin cruzar IPs")
	}

	catalog, err := s.DB.ListDispositivos()
	if err != nil {
		return Resultado{}, err
	}
	dispByNameIP := indexDispositivosByNameIP(catalog)
	log.Printf("Tabla dispositivos: %d filas (%d con nombre+IP indexables)", len(catalog), len(dispByNameIP))

	out := Resultado{
		CoveragesTotales:   len(coverages),
		InterMapperDevices: len(devices),
	}

	seen := make(map[string]struct{})
	rows := make([]models.DispositivoAP, 0, 256)

	for _, cov := range coverages {
		if err := ctx.Err(); err != nil {
			return Resultado{}, err
		}
		name := strings.TrimSpace(cov.Name)
		if !hasOSNPrefix(name) {
			continue
		}
		out.OSNProcesadas++

		parsed, ok := ParseCoverageName(name)
		if !ok {
			out.ParseFail++
			log.Printf("⚠️ name OSN- no parseable: %q", name)
			continue
		}

		siteName := siteByID[strings.TrimSpace(cov.TowerSiteID)]
		torre := TorreNombreDesdeSite(siteName, parsed.Sitio)
		if torre == "" {
			out.ParseFail++
			log.Printf("⚠️ sin torre_nombre (towersiteID=%s name=%q)", cov.TowerSiteID, name)
			continue
		}

		tipo := TipoConOMNI(parsed.Tipo, cov.BeamwidthFilter)
		row := models.DispositivoAP{
			TorreNombre: torre,
			APName:      parsed.AP,
			Tipo:        tipo,
			Azimut:      strings.TrimSpace(cov.AntennaAzimuth),
			Tilt:        strings.TrimSpace(cov.AntennaTilt),
			Altura:      strings.TrimSpace(cov.AntennaHeight),
		}

		if d, ok := MatchDevice(devices, parsed.AP); ok {
			row.IPAddress = strings.TrimSpace(d.Address)
		}
		if id, ok := lookupDispID(dispByNameIP, row.APName, row.IPAddress); ok {
			row.DispID = &id
		}

		key := strings.ToUpper(torre) + "\x00" + strings.ToUpper(parsed.AP)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if row.IPAddress != "" {
			out.ConIP++
		} else {
			out.SinIP++
		}
		if row.DispID != nil {
			out.ConDispID++
		}
		rows = append(rows, row)
	}

	if err := s.DB.ReplaceDispositivosAP(rows); err != nil {
		return Resultado{}, err
	}
	out.Insertadas = len(rows)
	log.Printf("dispositivos_ap recreada: %d filas (%d con IP, %d sin IP, %d con disp_id, %d OSN- , %d no parseadas)",
		out.Insertadas, out.ConIP, out.SinIP, out.ConDispID, out.OSNProcesadas, out.ParseFail)
	return out, nil
}
