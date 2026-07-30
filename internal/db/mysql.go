package db

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
	"tower-scraper/internal/config"

	"tower-scraper/internal/models"

	"github.com/go-sql-driver/mysql"
)

// Límites del pool. connMaxIdleTime y connMaxLifetime deben quedar por debajo del
// wait_timeout del servidor para no reutilizar conexiones que este ya cerró.
const (
	dbMaxOpenConns    = 20
	dbMaxIdleConns    = 5
	dbConnMaxIdleTime = time.Minute
	dbConnMaxLifetime = 3 * time.Minute
)

// Reintentos ante conexión cerrada por el servidor.
const (
	dbQueryAttempts = 3
	dbRetryDelay    = 100 * time.Millisecond
)

type APInfo struct {
	APName    string `json:"ap_name"`
	Tipo      string `json:"tipo"`
	Azimut    string `json:"azimut"`
	Tilt      string `json:"tilt"`
	Altura    string `json:"altura"`
	IPAddress string `json:"ip_address,omitempty"`
	Status    string `json:"status,omitempty"` // Para marcar si pasó la prueba de cobertura
}

type DBClient struct {
	conn *sql.DB
}

func mysqlAddr(host, port string) string {
	if port == "" {
		port = "3306"
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, port)
}

// NewDBClient inicializa la conexión usando DB_HOST, DB_PORT (por defecto 3306), DB_USER, DB_PASS y DB_NAME del entorno (.env).
func NewDBClient(cfg *config.Config) (*DBClient, error) {
	if cfg.DBHost == "" || cfg.DBUser == "" || cfg.DBName == "" {
		return nil, fmt.Errorf("faltan DB_HOST, DB_USER o DB_NAME en el entorno")
	}
	cnf := mysql.Config{
		User:                 cfg.DBUser,
		Passwd:               cfg.DBPass,
		Net:                  "tcp",
		Addr:                 mysqlAddr(cfg.DBHost, cfg.DBPort),
		DBName:               cfg.DBName,
		AllowNativePasswords: true,
		Timeout:              10 * time.Second,
	}
	db, err := sql.Open("mysql", cnf.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("error conectando a MySQL: %w", err)
	}
	db.SetMaxOpenConns(dbMaxOpenConns)
	db.SetMaxIdleConns(dbMaxIdleConns)
	db.SetConnMaxIdleTime(dbConnMaxIdleTime)
	db.SetConnMaxLifetime(dbConnMaxLifetime)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		if cfg.DBPass == "" {
			return nil, fmt.Errorf("ping MySQL falló con DB_PASS vacío (suele verse como 'using password: NO'); revisa .env y docker-compose: %w", err)
		}
		return nil, fmt.Errorf("error haciendo ping a MySQL: %w", err)
	}
	return &DBClient{conn: db}, nil
}

// isConnDeadErr identifica conexiones que el servidor ya cerró (wait_timeout,
// reinicio, kill). MySQL suele devolver 2006/2013; 4031 también se observa en
// algunos despliegues compatibles. database/sql no los reintenta por su cuenta.
func isConnDeadErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn) || errors.Is(err, io.EOF) {
		return true
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		switch myErr.Number {
		case 1927, 2006, 2013, 4031:
			return true
		}
	}
	return false
}

func (c *DBClient) query(query string, args ...any) (*sql.Rows, error) {
	var rows *sql.Rows
	var err error
	for attempt := 0; attempt < dbQueryAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(dbRetryDelay)
		}
		rows, err = c.conn.Query(query, args...)
		if !isConnDeadErr(err) {
			return rows, err
		}
	}
	return rows, err
}

func (c *DBClient) queryRowScan(query string, args []any, dest ...any) error {
	var err error
	for attempt := 0; attempt < dbQueryAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(dbRetryDelay)
		}
		err = c.conn.QueryRow(query, args...).Scan(dest...)
		if !isConnDeadErr(err) {
			return err
		}
	}
	return err
}

// ObtenerAPsPorTorre cruza la tabla de torres_ap con ap_info
func (c *DBClient) ObtenerAPsPorTorre(nombreTorreTC string) ([]APInfo, error) {
	nombreLimpio := strings.ReplaceAll(nombreTorreTC, "OSN.", "")
	nombreLimpio = strings.TrimSpace(nombreLimpio)

	// CAMBIO 1: Reemplazar 'LIKE' por '=' en la consulta SQL
	query := `SELECT a.ap_name, a.azimut, a.tilt, a.altura, a.tipo, a.ip_address
          FROM dispositivos_ap a 
          WHERE a.torre_nombre = ?`

	// CAMBIO 2: Quitar los comodines "%" para hacer una búsqueda exacta
	searchParam := nombreLimpio

	rows, err := c.query(query, searchParam)
	if err != nil {
		return nil, fmt.Errorf("error consultando APs: %w", err)
	}
	defer rows.Close()

	var aps []APInfo
	for rows.Next() {
		var ap APInfo
		var azimut, tilt, altura, ip sql.NullString
		if err := rows.Scan(&ap.APName, &azimut, &tilt, &altura, &ap.Tipo, &ip); err != nil {
			return nil, err
		}
		ap.Azimut = nullStringValue(azimut)
		ap.Tilt = nullStringValue(tilt)
		ap.Altura = nullStringValue(altura)
		if ip.Valid {
			ap.IPAddress = strings.TrimSpace(ip.String)
		}
		aps = append(aps, ap)
	}
	return aps, nil
}

func GetAPsByTower(db *sql.DB, towerName string) ([]models.AccessPoint, error) {
	query := `
		SELECT id, torre_nombre, ap_name, tipo, azimut, tilt, altura, ip_address 
		FROM dispositivos_ap 
		WHERE torre_nombre = ?`

	rows, err := db.Query(query, towerName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aps []models.AccessPoint
	for rows.Next() {
		var ap models.AccessPoint
		var azimut, tilt, altura, ip sql.NullString

		err := rows.Scan(
			&ap.ID, &ap.TowerName, &ap.APName, &ap.Tipo,
			&azimut, &tilt, &altura, &ip,
		)
		if err != nil {
			return nil, err
		}

		ap.Azimut = nullStringValue(azimut)
		ap.Tilt = nullStringValue(tilt)
		ap.Altura = nullStringValue(altura)
		if ip.Valid {
			ap.IPAddress = strings.TrimSpace(ip.String)
		}

		aps = append(aps, ap)
	}
	return aps, nil
}

func nullStringValue(ns sql.NullString) string {
	if ns.Valid {
		return strings.TrimSpace(ns.String)
	}
	return ""
}
