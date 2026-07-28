package models

// TorreDB fila de la tabla torres.
type TorreDB struct {
	ID       int    `json:"id"`
	Nombre   string `json:"nombre"`
	Latitud  string `json:"latitud,omitempty"`
	Longitud string `json:"longitud,omitempty"`
}
