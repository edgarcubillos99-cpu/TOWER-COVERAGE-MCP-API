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

	rows, err := c.conn.Query(query, args...)
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
