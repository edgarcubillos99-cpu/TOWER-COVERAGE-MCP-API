package apsync

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Device es un equipo leído de InterMapper (solo GET/export).
type Device struct {
	ID      string
	Name    string
	Address string
}

// InterMapperClient exporta la tabla devices por HTTP. Nunca escribe ni cambia configuración.
type InterMapperClient struct {
	BaseURL    string
	User       string
	Password   string
	HTTP       *http.Client
	ExportPath string
}

func NewInterMapperClient(baseURL, user, password string, skipTLSVerify bool) *InterMapperClient {
	baseURL = strings.TrimSpace(baseURL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if skipTLSVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opcional para certs internos
	}
	return &InterMapperClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		User:       strings.TrimSpace(user),
		Password:   password,
		ExportPath: "/~export/devices.json?fields=id,name,address",
		HTTP: &http.Client{
			Timeout:   90 * time.Second,
			Transport: transport,
		},
	}
}

func (c *InterMapperClient) exportURL() string {
	raw := strings.TrimSpace(c.BaseURL)
	if raw == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(raw), "/~export/") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return raw + c.ExportPath
	}
	return strings.TrimRight(raw, "/") + c.ExportPath
}

// FetchDevices GET del export JSON. Solo lectura.
func (c *InterMapperClient) FetchDevices() ([]Device, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return nil, fmt.Errorf("INTERMAPPER_URL no está definida")
	}
	endpoint := c.exportURL()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creando request InterMapper: %w", err)
	}
	if c.User != "" {
		req.SetBasicAuth(c.User, c.Password)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error llamando InterMapper (solo lectura): %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo InterMapper: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("InterMapper respondió %d: %s", resp.StatusCode, truncateBody(body))
	}
	devs, err := parseInterMapperDevices(body)
	if err != nil {
		return nil, err
	}
	return devs, nil
}

func parseInterMapperDevices(body []byte) ([]Device, error) {
	body = bytesTrimBOM(body)
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("error parseando JSON InterMapper: %w", err)
	}
	items := flattenDeviceList(raw)
	out := make([]Device, 0, len(items))
	for _, item := range items {
		d := Device{
			ID:      pickMapString(item, "id", "Id", "ID", "deviceid", "DeviceID"),
			Name:    pickMapString(item, "name", "Name", "device", "Device"),
			Address: pickMapString(item, "address", "Address", "ip", "IP", "ipaddress", "IPAddress"),
		}
		if strings.TrimSpace(d.Name) == "" && strings.TrimSpace(d.Address) == "" {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

func flattenDeviceList(raw any) []map[string]any {
	switch v := raw.(type) {
	case []any:
		return mapsFromSlice(v)
	case map[string]any:
		for _, k := range []string{"devices", "Devices", "rows", "data", "items"} {
			if inner, ok := v[k]; ok {
				if list := flattenDeviceList(inner); len(list) > 0 {
					return list
				}
			}
		}
		return []map[string]any{v}
	default:
		return nil
	}
}

func mapsFromSlice(in []any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		m, ok := item.(map[string]any)
		if ok {
			out = append(out, m)
		}
	}
	return out
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
			return anyString(v)
		}
	}
	return ""
}

func anyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return strings.TrimSpace(x.String())
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func bytesTrimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		return b[3:]
	}
	return b
}

func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 180 {
		return s[:180] + "…"
	}
	return s
}

// MatchDevice localiza el dispositivo InterMapper cuyo name corresponde al AP.
func MatchDevice(devices []Device, apName string) (Device, bool) {
	ap := strings.TrimSpace(apName)
	if ap == "" || len(devices) == 0 {
		return Device{}, false
	}
	apNorm := strings.ToUpper(ap)

	var exact []Device
	var token []Device
	var contains []Device
	for _, d := range devices {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			continue
		}
		n := strings.ToUpper(name)
		if n == apNorm {
			exact = append(exact, d)
			continue
		}
		if hasAPToken(n, apNorm) {
			token = append(token, d)
			continue
		}
		if strings.Contains(n, apNorm) {
			contains = append(contains, d)
		}
	}
	for _, group := range [][]Device{exact, token, contains} {
		if d, ok := pickBestDevice(group, apNorm); ok {
			return d, true
		}
	}
	return Device{}, false
}

func hasAPToken(nameUpper, apUpper string) bool {
	if nameUpper == "" || apUpper == "" {
		return false
	}
	for _, sep := range []string{" ", "\t"} {
		for _, part := range strings.Split(nameUpper, sep) {
			part = strings.Trim(part, "-_/")
			if part == apUpper {
				return true
			}
			if strings.HasSuffix(part, "-"+apUpper) || strings.HasPrefix(part, apUpper+"-") {
				return true
			}
		}
	}
	if strings.HasSuffix(nameUpper, "-"+apUpper) || strings.HasSuffix(nameUpper, " "+apUpper) {
		return true
	}
	return false
}

func pickBestDevice(devs []Device, apUpper string) (Device, bool) {
	if len(devs) == 0 {
		return Device{}, false
	}
	if len(devs) == 1 {
		return devs[0], true
	}
	best := devs[0]
	bestScore := deviceScore(best, apUpper)
	for _, d := range devs[1:] {
		if s := deviceScore(d, apUpper); s > bestScore {
			best = d
			bestScore = s
		}
	}
	return best, true
}

func deviceScore(d Device, apUpper string) int {
	n := strings.ToUpper(strings.TrimSpace(d.Name))
	score := 0
	if n == apUpper {
		score += 100
	}
	if strings.HasSuffix(n, apUpper) {
		score += 20
	}
	if strings.TrimSpace(d.Address) != "" {
		score += 5
	}
	score -= len(n) / 20
	return score
}
