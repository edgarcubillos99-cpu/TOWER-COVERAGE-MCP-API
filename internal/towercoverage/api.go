package towercoverage

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMultiCoverageID = "31710"
	apiBaseURL             = "https://api.towercoverage.com/towercoverage.asmx/EUSPrequalAPIPlusCoverageId"
	maxDistanceMiles       = 6.0
)

var reCoverageEntry = regexp.MustCompile(`^(Yes|No):\s*Coverage ID:(\d+)\s+Coverage Name:(.+?)\s+Signal Beam:([-\d.]+)\s+has a\s+(.+)$`)

// PrequalEntry representa una torre devuelta por la API de prequalificación.
type PrequalEntry struct {
	Approved     bool
	CoverageID   string
	CoverageName string
	SignalBeam   string
	LinkStatus   string
}

// Client consulta la API REST de TowerCoverage.
type Client struct {
	Account         string
	Key             string
	MultiCoverageID string
	HTTP            *http.Client
}

func NewClient(account, key, multiCoverageID string) *Client {
	if strings.TrimSpace(multiCoverageID) == "" {
		multiCoverageID = defaultMultiCoverageID
	}
	return &Client{
		Account:         strings.TrimSpace(account),
		Key:             strings.TrimSpace(key),
		MultiCoverageID: strings.TrimSpace(multiCoverageID),
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) FetchPrequal(lat, lon string) (string, error) {
	if c.Account == "" || c.Key == "" {
		return "", fmt.Errorf("faltan TOWER_API_ACCOUNT o TOWER_API_KEY en el entorno")
	}

	params := url.Values{}
	params.Set("multicoverageid", c.MultiCoverageID)
	params.Set("Account", c.Account)
	params.Set("Address", "")
	params.Set("city", "")
	params.Set("Country", "")
	params.Set("State", "")
	params.Set("zipcode", "")
	params.Set("Latitude", strings.TrimSpace(lat))
	params.Set("Longitude", strings.TrimSpace(lon))
	params.Set("RxMargin", "0")
	params.Set("key", c.Key)

	reqURL := apiBaseURL + "?" + params.Encode()
	resp, err := c.HTTP.Get(reqURL)
	if err != nil {
		return "", fmt.Errorf("error llamando API TowerCoverage: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error leyendo respuesta API: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API TowerCoverage respondió %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return extractXMLStringContent(body)
}

func extractXMLStringContent(body []byte) (string, error) {
	var wrapped struct {
		Content string `xml:",chardata"`
	}
	if err := xml.Unmarshal(body, &wrapped); err == nil && strings.TrimSpace(wrapped.Content) != "" {
		return strings.TrimSpace(wrapped.Content), nil
	}

	raw := string(body)
	start := strings.Index(raw, ">")
	end := strings.LastIndex(raw, "<")
	if start >= 0 && end > start {
		return strings.TrimSpace(raw[start+1 : end]), nil
	}
	return "", fmt.Errorf("respuesta XML inválida de TowerCoverage")
}

// ParsePrequalResponse divide el texto de la API en entradas estructuradas.
func ParsePrequalResponse(raw string) []PrequalEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var out []PrequalEntry
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		m := reCoverageEntry.FindStringSubmatch(part)
		if len(m) != 6 {
			continue
		}
		out = append(out, PrequalEntry{
			Approved:     strings.EqualFold(m[1], "Yes"),
			CoverageID:   m[2],
			CoverageName: strings.TrimSpace(m[3]),
			SignalBeam:   strings.TrimSpace(m[4]),
			LinkStatus:   strings.TrimSpace(m[5]),
		})
	}
	return out
}

func isGoodLink(entry PrequalEntry) bool {
	return entry.Approved && strings.EqualFold(entry.LinkStatus, "Good Link")
}

func formatSignal(beam string) string {
	beam = strings.TrimSpace(beam)
	if beam == "" {
		return ""
	}
	if f, err := strconv.ParseFloat(beam, 64); err == nil {
		return fmt.Sprintf("%.1f dBm", f)
	}
	return beam
}
