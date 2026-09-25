package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// DispositivoCatalogo es una fila de la tabla dispositivos (catálogo InterMapper/NOC).
type DispositivoCatalogo struct {
	ID          int64
	Dispositivo string
	IPAddress   string
}

// ListDispositivos carga id, dispositivo e ip_address de la tabla dispositivos.
func (c *DBClient) ListDispositivos() ([]DispositivoCatalogo, error) {
	rows, err := c.query(`SELECT id, dispositivo, ip_address FROM dispositivos`)
	if err != nil {
		return nil, fmt.Errorf("error consultando dispositivos: %w", err)
	}
	defer rows.Close()

	out := make([]DispositivoCatalogo, 0, 1024)
	for rows.Next() {
		var d DispositivoCatalogo
		var ip sql.NullString
		if err := rows.Scan(&d.ID, &d.Dispositivo, &ip); err != nil {
			return nil, err
		}
		d.Dispositivo = strings.TrimSpace(d.Dispositivo)
		if ip.Valid {
			d.IPAddress = strings.TrimSpace(ip.String)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
