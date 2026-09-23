package towercoverage

import (
	"encoding/json"
	"testing"
)

func TestCoverageUnmarshalFlexible(t *testing.T) {
	raw := []byte(`{
		"id": 201,
		"name": "OSN-COLLORES TOWER-OSNAP16-A ePMP3000",
		"towersiteID": "5586",
		"antennaheight": "30",
		"antennaAzimuth": "76",
		"antennaTilt": "-2",
		"beamwidthFilter": "360"
	}`)
	var c Coverage
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.Name == "" || c.TowerSiteID != "5586" || c.AntennaHeight != "30" {
		t.Fatalf("%+v", c)
	}
	if c.AntennaAzimuth != "76" || c.AntennaTilt != "-2" || c.BeamwidthFilter != "360" {
		t.Fatalf("%+v", c)
	}
}
