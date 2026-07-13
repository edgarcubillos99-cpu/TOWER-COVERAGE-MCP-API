package db

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"tower-scraper/internal/models"
)

var dispositivoAPFilterColumns = map[string]string{
	"id":           "id",
	"disp_id":      "disp_id",
	"torre_nombre": "torre_nombre",
	"ap_name":      "ap_name",
	"tipo":         "tipo",
	"azimut":       "azimut",
	"tilt":         "tilt",
	"altura":       "altura",
	"ip_address":   "ip_address",
}

// ListDispositivosAP devuelve filas de dispositivos_ap filtradas por columnas (query params).
func (c *DBClient) ListDispositivosAP(filters map[string]string) ([]models.DispositivoAP, error) {
	query := `SELECT id, disp_id, torre_nombre, ap_name, tipo, azimut, tilt, altura, ip_address FROM dispositivos_ap`
	var args []any
	var clauses []string

	for key, col := range dispositivoAPFilterColumns {
		val, ok := filters[key]
		if !ok || strings.TrimSpace(val) == "" {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "id", "disp_id":
			if _, err := strconv.Atoi(val); err != nil {
				return nil, fmt.Errorf("%s debe ser un número entero", key)
			}
			clauses = append(clauses, col+" = ?")
			args = append(args, val)
		case "torre_nombre", "ap_name":
			clauses = append(clauses, col+" LIKE ?")
			args = append(args, "%"+val+"%")
		default:
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
		return nil, fmt.Errorf("error consultando dispositivos_ap: %w", err)
	}
	defer rows.Close()

	return scanDispositivosAP(rows)
}

func scanDispositivosAP(rows *sql.Rows) ([]models.DispositivoAP, error) {
	var out []models.DispositivoAP
	for rows.Next() {
		var d models.DispositivoAP
		var dispID sql.NullInt64
		var azimut, tilt, altura, ip sql.NullString
		if err := rows.Scan(
			&d.ID, &dispID, &d.TorreNombre, &d.APName, &d.Tipo,
			&azimut, &tilt, &altura, &ip,
		); err != nil {
			return nil, err
		}
		if dispID.Valid {
			v := int(dispID.Int64)
			d.DispID = &v
		}
		d.Azimut = nullStringValue(azimut)
		d.Tilt = nullStringValue(tilt)
		d.Altura = nullStringValue(altura)
		if ip.Valid {
			d.IPAddress = strings.TrimSpace(ip.String)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ResolveDispositivoAP localiza un AP por IP exacta o por nombre (opcionalmente acotado a torre).
func (c *DBClient) ResolveDispositivoAP(ipAddress, apName, torreNombre string) (models.DispositivoAP, error) {
	ipAddress = strings.TrimSpace(ipAddress)
	apName = strings.TrimSpace(apName)
	torreNombre = strings.TrimSpace(torreNombre)

	switch {
	case ipAddress != "":
		rows, err := c.ListDispositivosAP(map[string]string{"ip_address": ipAddress})
		if err != nil {
			return models.DispositivoAP{}, err
		}
		if len(rows) == 0 {
			return models.DispositivoAP{}, fmt.Errorf("no hay dispositivo con ip_address %q", ipAddress)
		}
		return rows[0], nil

	case apName != "":
		filters := map[string]string{"ap_name": apName}
		if torreNombre != "" {
			filters["torre_nombre"] = torreNombre
		}
		rows, err := c.ListDispositivosAP(filters)
		if err != nil {
			return models.DispositivoAP{}, err
		}
		if len(rows) == 0 {
			return models.DispositivoAP{}, fmt.Errorf("no hay dispositivo con ap_name %q", apName)
		}
		if len(rows) > 1 {
			return models.DispositivoAP{}, fmt.Errorf(
				"ap_name %q coincide con %d torres; indica torre_nombre para desambiguar",
				apName, len(rows),
			)
		}
		return rows[0], nil

	default:
		return models.DispositivoAP{}, fmt.Errorf("indica ip_address o ap_name")
	}
}
