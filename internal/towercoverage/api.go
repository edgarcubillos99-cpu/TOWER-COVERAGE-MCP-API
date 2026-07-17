package towercoverage

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	siteListURL      = "https://api.towercoverage.com/Sites/GetSiteList"
	linkPathURL      = "https://api.towercoverage.com/Links/LinkPathAPI"
	maxDistanceMiles = 6.0

	// Parámetros fijos de LinkPathAPI (solo cambian Site1* y Site2 lat/lon).
	defaultLinkName           = "CONSULTA"
	defaultSite2AntennaHeight = "6"
	defaultFrequencyID        = "52"
	defaultTXPower            = "25"
	defaultTxAntennaGain      = "25"
	defaultTxLineLoss         = "0.5"
	defaultRxAntennaGain      = "25"
	defaultRxLineLoss         = "0.5"
	defaultRxThreshold        = "-72"
	defaultReliability        = "80"
	defaultLandCoverID        = "876"
)

// Site es un sitio/torre devuelto por GetSiteList.
type Site struct {
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Group       string  `json:"group"`
	Height      float64 `json:"height"`
	Description string  `json:"description"`
	LastUpdated string  `json:"lastupdated"`
	ID          int     `json:"id"`
	AccountID   int     `json:"accountId"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Errors      string  `json:"errors"`
	Error       string  `json:"error"`
}

// LinkPathResult es la respuesta JSON de LinkPathAPI.
// La API real usa claves en minúsculas (signalmargin, error, …); la doc a veces
// muestra PascalCase (Signalmargin, Error) — aceptamos ambas.
type LinkPathResult struct {
	LinkID                   string `json:"linkid"`
	LeftsiteName             string `json:"leftsite_Name"`
	LeftsiteLatitude         string `json:"leftsite_Latitude"`
	LeftsiteLongitude        string `json:"leftsite_Longitude"`
	LeftsiteGroundElevation  string `json:"leftsite_groundelevation"`
	LeftsiteAntennaHeight    string `json:"leftsite_Antennaheight"`
	LeftsiteLinkAzimuth      string `json:"leftsite_linkazimuth"`
	LeftsiteLinkTilt         string `json:"leftsite_linktilt"`
	LeftsideAntennaGain      string `json:"leftside_antennagain"`
	RightsiteName            string `json:"rightsite_Name"`
	RightsiteLatitude        string `json:"rightsite_Latitude"`
	RightsiteLongitude       string `json:"rightsite_Longitude"`
	RightsiteGroundElevation string `json:"rightsite_groundelevation"`
	RightsiteAntennaHeight   string `json:"rightsite_Antennaheight"`
	RightsiteLinkAzimuth     string `json:"rightsite_linkazimuth"`
	RightsiteLinkTilt        string `json:"rightsite_linktilt"`
	RightsiteAntennaGain     string `json:"rightsite_antennagain"`
	RxSensitivity            string `json:"rxsensitivity"`
	SignalMargin             string `json:"-"`
	SignalInDBm              string `json:"-"`
	ServiceQuality           string `json:"-"`
	Distance                 string `json:"-"`
	MinimumAntennaHeight     string `json:"-"`
	DiskTime                 string `json:"disktime"`
	CalcTime                 string `json:"calcTime"`
	PathImage                string `json:"-"`
	MapDetails               string `json:"mapdetails"`
	Availability             *string `json:"availability"`
	SignalWithRain           *string `json:"signalWithRain"`
	Error                    string  `json:"-"`
}

func (r *LinkPathResult) UnmarshalJSON(data []byte) error {
	type raw map[string]json.RawMessage
	var m raw
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k]; ok {
				var s string
				if err := json.Unmarshal(v, &s); err == nil {
					return s
				}
			}
		}
		return ""
	}

	r.LinkID = get("linkid", "Linkid")
	r.LeftsiteName = get("leftsite_Name", "Leftsite_Name")
	r.LeftsiteLatitude = get("leftsite_Latitude", "Leftsite_Latitude")
	r.LeftsiteLongitude = get("leftsite_Longitude", "Leftsite_Longitude")
	r.LeftsiteGroundElevation = get("leftsite_groundelevation", "Leftsite_groundelevation")
	r.LeftsiteAntennaHeight = get("leftsite_Antennaheight", "Leftsite_Antennaheight")
	r.LeftsiteLinkAzimuth = get("leftsite_linkazimuth", "Leftsite_linkazimuth")
	r.LeftsiteLinkTilt = get("leftsite_linktilt", "Leftsite_linktilt")
	r.LeftsideAntennaGain = get("leftside_antennagain", "leftside_antennagain")
	r.RightsiteName = get("rightsite_Name", "Rightsite_Name")
	r.RightsiteLatitude = get("rightsite_Latitude", "Rightsite_Latitude")
	r.RightsiteLongitude = get("rightsite_Longitude", "Rightsite_Longitude")
	r.RightsiteGroundElevation = get("rightsite_groundelevation", "Rightsite_groundelevation")
	r.RightsiteAntennaHeight = get("rightsite_Antennaheight", "Rightsite_Antennaheight")
	r.RightsiteLinkAzimuth = get("rightsite_linkazimuth", "Rightsite_linkazimuth")
	r.RightsiteLinkTilt = get("rightsite_linktilt", "Rightsite_linktilt")
	r.RightsiteAntennaGain = get("rightsite_antennagain", "Rightsite_antennagain")
	r.RxSensitivity = get("rxsensitivity", "Rxsensitivity")
	r.SignalMargin = get("signalmargin", "Signalmargin")
	r.SignalInDBm = get("signalindBm", "SignalindBm")
	r.ServiceQuality = get("servicequality", "Servicequality")
	r.Distance = get("distance", "Distance")
	r.MinimumAntennaHeight = get("minimumantennaheight", "Minimumantennaheight")
	r.DiskTime = get("disktime")
	r.CalcTime = get("calcTime")
	r.PathImage = get("pathimage", "Pathimage")
	r.MapDetails = get("mapdetails")
	r.Error = get("error", "Error")

	if v, ok := m["availability"]; ok {
		var s *string
		_ = json.Unmarshal(v, &s)
		r.Availability = s
	}
	if v, ok := m["signalWithRain"]; ok {
		var s *string
		_ = json.Unmarshal(v, &s)
		r.SignalWithRain = s
	}
	return nil
}

// Client consulta la API REST de TowerCoverage.
type Client struct {
	Account   string
	Key       string
	HTTP      *http.Client
	SiteStore *SiteListStore
}

func NewClient(account, key string) *Client {
	return &Client{
		Account: strings.TrimSpace(account),
		Key:     strings.TrimSpace(key),
		HTTP: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (c *Client) setAuthHeaders(req *http.Request) {
	req.Header.Set("X-TowerCoverage-Account-Id", c.Account)
	req.Header.Set("X-TowerCoverage-Api-Key", c.Key)
}

// FetchSiteList obtiene el listado de torres de la cuenta.
func (c *Client) FetchSiteList() ([]Site, error) {
	if c.Account == "" || c.Key == "" {
		return nil, fmt.Errorf("faltan TOWER_API_ACCOUNT o TOWER_API_KEY en el entorno")
	}

	req, err := http.NewRequest(http.MethodGet, siteListURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creando request GetSiteList: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error llamando GetSiteList: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo GetSiteList: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetSiteList respondió %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var sites []Site
	if err := json.Unmarshal(body, &sites); err != nil {
		return nil, fmt.Errorf("error parseando GetSiteList: %w", err)
	}
	return sites, nil
}

// FetchLinkPath calcula el enlace RF entre torre (Site1) y cliente (Site2)
// vía POST /Links/LinkPathAPI.
func (c *Client) FetchLinkPath(site1ID int, site1Lat, site1Lon, site1AntennaHeight, site2Lat, site2Lon string) (*LinkPathResult, error) {
	if c.Account == "" || c.Key == "" {
		return nil, fmt.Errorf("faltan TOWER_API_ACCOUNT o TOWER_API_KEY en el entorno")
	}

	params := url.Values{}
	params.Set("LinkName", defaultLinkName)
	params.Set("Linkid", "0")
	params.Set("Site1id", strconv.Itoa(site1ID))
	params.Set("Site1Latitude", strings.TrimSpace(site1Lat))
	params.Set("Site1Longitude", strings.TrimSpace(site1Lon))
	params.Set("Site1AntennaHeight", strings.TrimSpace(site1AntennaHeight))
	params.Set("Site2id", "0")
	params.Set("Site2Latitude", strings.TrimSpace(site2Lat))
	params.Set("Site2Longitude", strings.TrimSpace(site2Lon))
	params.Set("Site2AntennaHeight", defaultSite2AntennaHeight)
	params.Set("Frequencyid", defaultFrequencyID)
	params.Set("TXPower", defaultTXPower)
	params.Set("TxAntennaGain", defaultTxAntennaGain)
	params.Set("TxLineLoss", defaultTxLineLoss)
	params.Set("RxAntennaGain", defaultRxAntennaGain)
	params.Set("RxLineLoss", defaultRxLineLoss)
	params.Set("RxThreshold", defaultRxThreshold)
	params.Set("Reliability", defaultReliability)
	params.Set("UseLandCover", "1")
	params.Set("LandCover", "1")
	params.Set("LandCoverID", defaultLandCoverID)
	params.Set("UseTwoRays", "0")
	params.Set("Savelinkindatabase", "0")

	req, err := http.NewRequest(http.MethodPost, linkPathURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("error creando request LinkPathAPI: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setAuthHeaders(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error llamando LinkPathAPI: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo LinkPathAPI: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LinkPathAPI respondió %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result LinkPathResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("error parseando LinkPathAPI: %w", err)
	}
	return &result, nil
}

// isPossibleLink indica si el path analysis produjo un enlace viable.
// Se descarta si la API reporta error o si el margen de señal es negativo
// (señal por debajo del RxThreshold configurado).
func isPossibleLink(result *LinkPathResult) bool {
	if result == nil {
		return false
	}
	if strings.TrimSpace(result.Error) != "" {
		return false
	}
	margin, err := strconv.ParseFloat(strings.TrimSpace(result.SignalMargin), 64)
	if err != nil {
		return false
	}
	return margin >= 0
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

func formatCoord(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func formatHeight(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
