package models

// DispositivoAP fila de la tabla dispositivos_ap.
type DispositivoAP struct {
	ID          int    `json:"id"`
	DispID      *int   `json:"disp_id,omitempty"`
	TorreNombre string `json:"torre_nombre"`
	APName      string `json:"ap_name"`
	Tipo        string `json:"tipo"`
	Azimut      string `json:"azimut"`
	Tilt        string `json:"tilt"`
	Altura      string `json:"altura"`
	IPAddress   string `json:"ip_address,omitempty"`
}
