package db

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"tower-scraper/internal/models"
)

var torreFilterColumns = map[string]string{
	"id":       "id",
	"nombre":   "nombre",
	"latitud":  "latitud",
	"longitud": "longitud",
}

// ListTorres devuelve filas de torres filtradas por columnas de la tabla.
func (c *DBClient) ListTorres(filters map[string]string) ([]models.TorreDB, error) {
	query := `SELECT id, nombre, latitud, longitud FROM torres`
	var args []any
	var clauses []string

	for key, col := range torreFilterColumns {
		val, ok := filters[key]
		if !ok || strings.TrimSpace(val) == "" {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "id":
			if _, err := strconv.Atoi(val); err != nil {
				return nil, fmt.Errorf("id debe ser un número entero")
			}
			clauses = append(clauses, col+" = ?")
			args = append(args, val)
		case "nombre":
			clauses = append(clauses, col+" LIKE ?")
			args = append(args, "%"+val+"%")
		case "latitud", "longitud":
			clauses = append(clauses, col+" = ?")
			args = append(args, val)
		}
	}

	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY id"

	rows, err := c.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("error consultando torres: %w", err)
	}
	defer rows.Close()

	var out []models.TorreDB
	for rows.Next() {
		var t models.TorreDB
		var lat, lon sql.NullString
		if err := rows.Scan(&t.ID, &t.Nombre, &lat, &lon); err != nil {
			return nil, err
		}
		t.Latitud = nullStringValue(lat)
		t.Longitud = nullStringValue(lon)
		out = append(out, t)
	}
	return out, rows.Err()
}

// NombresTorresSet carga todos los nombres de la tabla torres en un set para
// lookup O(1). Incluye cada nombre tal cual y, si no empieza por OSN., también
// la variante con prefijo "OSN." para casar con nombres de la API.
func (c *DBClient) NombresTorresSet() (map[string]struct{}, error) {
	rows, err := c.query(`SELECT nombre FROM torres`)
	if err != nil {
		return nil, fmt.Errorf("error consultando nombres de torres: %w", err)
	}
	defer rows.Close()

	out := make(map[string]struct{})
	for rows.Next() {
		var nombre string
		if err := rows.Scan(&nombre); err != nil {
			return nil, err
		}
		nombre = strings.TrimSpace(nombre)
		if nombre == "" {
			continue
		}
		out[nombre] = struct{}{}
		if !strings.HasPrefix(nombre, "OSN.") {
			out["OSN."+nombre] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// TorreEnSet indica si el nombre de TowerCoverage está en el set de BD
// (nombre exacto o sin prefijo OSN.).
func TorreEnSet(nameSet map[string]struct{}, nombreTC string) bool {
	nombreTC = strings.TrimSpace(nombreTC)
	if nombreTC == "" {
		return false
	}
	if _, ok := nameSet[nombreTC]; ok {
		return true
	}
	if stripped := strings.TrimSpace(strings.TrimPrefix(nombreTC, "OSN.")); stripped != "" && stripped != nombreTC {
		_, ok := nameSet[stripped]
		return ok
	}
	return false
}

// ObtenerTorrePorNombre busca una torre por nombre exacto o sin prefijo OSN.
// En BD los nombres se guardan sin el prefijo "OSN." que usa la API de TowerCoverage.
func (c *DBClient) ObtenerTorrePorNombre(nombreTC string) (*models.TorreDB, error) {
	nombreTC = strings.TrimSpace(nombreTC)
	if nombreTC == "" {
		return nil, fmt.Errorf("nombre de torre vacío")
	}

	candidates := []string{nombreTC}
	if stripped := strings.TrimSpace(strings.TrimPrefix(nombreTC, "OSN.")); stripped != "" && stripped != nombreTC {
		candidates = append(candidates, stripped)
	}

	seen := make(map[string]struct{})
	for _, name := range candidates {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		query := `SELECT id, nombre, latitud, longitud FROM torres WHERE nombre = ? LIMIT 1`
		var t models.TorreDB
		var lat, lon sql.NullString
		err := c.queryRowScan(query, []any{name}, &t.ID, &t.Nombre, &lat, &lon)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("error consultando torre %q: %w", name, err)
		}
		t.Latitud = nullStringValue(lat)
		t.Longitud = nullStringValue(lon)
		return &t, nil
	}

	return nil, fmt.Errorf("torre %q no encontrada en tabla torres", nombreTC)
}
