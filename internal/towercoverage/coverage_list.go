package towercoverage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Coverage es un elemento de GET /Coverage/GetCoverageList.
type Coverage struct {
	ID              string
	Name            string
	TowerSiteID     string
	AntennaHeight   string
	AntennaAzimuth  string
	AntennaTilt     string
	BeamwidthFilter string
}

func (c *Coverage) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.ID = pickMapString(raw, "id")
	c.Name = pickMapString(raw, "name")
	c.TowerSiteID = pickMapString(raw, "towersiteID", "towerSiteID", "towersiteid", "TowerSiteID")
	c.AntennaHeight = pickMapString(raw, "antennaheight", "antennaHeight", "Antennaheight")
	c.AntennaAzimuth = pickMapString(raw, "antennaAzimuth", "antennaazimuth", "AntennaAzimuth")
	c.AntennaTilt = pickMapString(raw, "antennaTilt", "antennatilt", "AntennaTilt")
	c.BeamwidthFilter = pickMapString(raw, "beamwidthFilter", "beamwidthfilter", "BeamwidthFilter")
	return nil
}

func pickMapString(m map[string]any, keys ...string) string {
	if len(m) == 0 {
		return ""
	}
	lower := make(map[string]any, len(m))
	for k, v := range m {
		lower[strings.ToLower(k)] = v
	}
	for _, k := range keys {
		if v, ok := lower[strings.ToLower(k)]; ok {
			return anyToTrimmedString(v)
		}
	}
	return ""
}

func anyToTrimmedString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return strings.TrimSpace(x.String())
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

// FetchCoverageList obtiene el listado de coberturas. No usa GetMapCoverages.
func (c *Client) FetchCoverageList() ([]Coverage, error) {
	if c.Account == "" || c.Key == "" {
		return nil, fmt.Errorf("faltan TOWER_API_ACCOUNT o TOWER_API_KEY en el entorno")
	}

	req, err := http.NewRequest(http.MethodGet, coverageListURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creando request GetCoverageList: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error llamando GetCoverageList: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo GetCoverageList: %w", err)
	}
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	trimmed := bytes.TrimSpace(body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetCoverageList respondió %d: %s", resp.StatusCode, truncateErrBody(trimmed))
	}
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("GetCoverageList devolvió cuerpo vacío")
	}
	if trimmed[0] != '[' && trimmed[0] != '{' {
		return nil, fmt.Errorf("GetCoverageList no devolvió JSON: %s", truncateErrBody(trimmed))
	}

	var list []Coverage
	if err := json.Unmarshal(trimmed, &list); err != nil {
		return nil, fmt.Errorf("error parseando GetCoverageList: %w", err)
	}
	return list, nil
}

func truncateErrBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 180 {
		return s[:180] + "…"
	}
	return s
}
