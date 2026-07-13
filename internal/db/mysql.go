package db

import (
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"
	"tower-scraper/internal/config"

	"tower-scraper/internal/models"

	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
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
	// Caché opcional en Redis. Si rdb es nil, todas las consultas van a MySQL.
	rdb      *redis.Client
	cacheTTL time.Duration
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
	}
	db, err := sql.Open("mysql", cnf.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("error conectando a MySQL: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		if cfg.DBPass == "" {
			return nil, fmt.Errorf("ping MySQL falló con DB_PASS vacío (suele verse como 'using password: NO'); revisa .env y docker-compose: %w", err)
		}
		return nil, fmt.Errorf("error haciendo ping a MySQL: %w", err)
	}
	return &DBClient{conn: db}, nil
}

// cleanTowerNameForDB replica la limpieza histórica del nombre para el WHERE de MySQL:
// quita el prefijo "OSN." y recorta espacios, preservando mayúsculas/minúsculas para no
// alterar el comportamiento de la colación de la base de datos.
func cleanTowerNameForDB(nombreTorreTC string) string {
	return strings.TrimSpace(strings.ReplaceAll(nombreTorreTC, "OSN.", ""))
}

// cacheKeyTowerName deriva la clave de Redis a partir del nombre ya limpio, en minúsculas
// para que la precarga (por torre_nombre de la BD) y la consulta (por nombre de TowerCoverage)
// coincidan siempre, sin depender de mayúsculas.
func cacheKeyTowerName(nombreLimpio string) string {
	return "tower_aps:" + strings.ToLower(strings.TrimSpace(nombreLimpio))
}

// ObtenerAPsPorTorre devuelve los APs de una torre. Si Redis está habilitado, intenta
// primero la caché; ante fallo o ausencia, cae a MySQL y repuebla la caché.
func (c *DBClient) ObtenerAPsPorTorre(nombreTorreTC string) ([]APInfo, error) {
	nombreLimpio := cleanTowerNameForDB(nombreTorreTC)
	key := cacheKeyTowerName(nombreLimpio)

	if aps, ok := c.apsFromCache(key); ok {
		return aps, nil
	}

	aps, err := c.obtenerAPsPorTorreDB(nombreLimpio)
	if err != nil {
		return nil, err
	}

	c.storeAPsInCache(key, aps)
	return aps, nil
}

// obtenerAPsPorTorreDB ejecuta la consulta real contra MySQL (fuente de verdad).
func (c *DBClient) obtenerAPsPorTorreDB(nombreLimpio string) ([]APInfo, error) {
	query := `SELECT a.ap_name, a.azimut, a.tilt, a.altura, a.tipo, a.ip_address
          FROM dispositivos_ap a 
          WHERE a.torre_nombre = ?`

	rows, err := c.conn.Query(query, nombreLimpio)
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
