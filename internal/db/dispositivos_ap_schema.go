package db

import (
	"fmt"
	"strings"

	"tower-scraper/internal/models"
)

const dispositivosAPCreateSQL = `CREATE TABLE dispositivos_ap (
  id INT NOT NULL AUTO_INCREMENT,
  disp_id INT NULL,
  torre_nombre VARCHAR(255) NOT NULL,
  ap_name VARCHAR(255) NOT NULL,
  tipo VARCHAR(255) NULL,
  azimut VARCHAR(64) NULL,
  tilt VARCHAR(64) NULL,
  altura VARCHAR(64) NULL,
  ip_address VARCHAR(64) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_torre_ap (torre_nombre, ap_name),
  KEY idx_torre_nombre (torre_nombre),
  KEY idx_ap_name (ap_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// ReplaceDispositivosAP recrea la tabla y carga las filas.
// Usa tabla temporal + RENAME para no dejar el catálogo vacío si el INSERT falla.
func (c *DBClient) ReplaceDispositivosAP(rows []models.DispositivoAP) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("cliente MySQL no inicializado")
	}

	const tmp = "dispositivos_ap_new"
	const old = "dispositivos_ap_old"

	if _, err := c.exec(`DROP TABLE IF EXISTS ` + tmp); err != nil {
		return fmt.Errorf("drop %s: %w", tmp, err)
	}
	if _, err := c.exec(`DROP TABLE IF EXISTS ` + old); err != nil {
		return fmt.Errorf("drop %s: %w", old, err)
	}

	create := strings.Replace(dispositivosAPCreateSQL, "CREATE TABLE dispositivos_ap", "CREATE TABLE "+tmp, 1)
	if _, err := c.exec(create); err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}

	if len(rows) > 0 {
		const chunk = 200
		for i := 0; i < len(rows); i += chunk {
			end := i + chunk
			if end > len(rows) {
				end = len(rows)
			}
			if err := c.insertDispositivosAP(tmp, rows[i:end]); err != nil {
				_, _ = c.exec(`DROP TABLE IF EXISTS ` + tmp)
				return err
			}
		}
	}

	exists, err := c.tableExists("dispositivos_ap")
	if err != nil {
		_, _ = c.exec(`DROP TABLE IF EXISTS ` + tmp)
		return err
	}
	if exists {
		if _, err := c.exec(`RENAME TABLE dispositivos_ap TO ` + old + `, ` + tmp + ` TO dispositivos_ap`); err != nil {
			_, _ = c.exec(`DROP TABLE IF EXISTS ` + tmp)
			return fmt.Errorf("rename dispositivos_ap: %w", err)
		}
		if _, err := c.exec(`DROP TABLE IF EXISTS ` + old); err != nil {
			return fmt.Errorf("drop %s: %w", old, err)
		}
	} else {
		if _, err := c.exec(`RENAME TABLE ` + tmp + ` TO dispositivos_ap`); err != nil {
			_, _ = c.exec(`DROP TABLE IF EXISTS ` + tmp)
			return fmt.Errorf("rename %s → dispositivos_ap: %w", tmp, err)
		}
	}
	return nil
}

func (c *DBClient) insertDispositivosAP(table string, rows []models.DispositivoAP) error {
	if len(rows) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(table)
	b.WriteString(" (disp_id, torre_nombre, ap_name, tipo, azimut, tilt, altura, ip_address) VALUES ")
	args := make([]any, 0, len(rows)*8)
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("(?,?,?,?,?,?,?,?)")
		var disp any
		if r.DispID != nil {
			disp = *r.DispID
		}
		args = append(args, disp, r.TorreNombre, r.APName, nullIfEmpty(r.Tipo),
			nullIfEmpty(r.Azimut), nullIfEmpty(r.Tilt), nullIfEmpty(r.Altura), nullIfEmpty(r.IPAddress))
	}
	if _, err := c.exec(b.String(), args...); err != nil {
		return fmt.Errorf("insert %s: %w", table, err)
	}
	return nil
}

func (c *DBClient) tableExists(name string) (bool, error) {
	var n int
	err := c.queryRowScan(
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`,
		[]any{name},
		&n,
	)
	if err != nil {
		return false, fmt.Errorf("comprobando tabla %s: %w", name, err)
	}
	return n > 0, nil
}

func nullIfEmpty(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}
