package apsync

import "testing"

func TestParseInterMapperDevices(t *testing.T) {
	raw := []byte(`[{"id":"12","name":"OSNAP16-A","address":"10.1.2.3"},{"Name":"CAP2","Address":"10.9.9.9"}]`)
	devs, err := parseInterMapperDevices(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 || devs[0].Address != "10.1.2.3" || devs[1].Name != "CAP2" {
		t.Fatalf("%+v", devs)
	}
}

func TestMatchDevice(t *testing.T) {
	devs := []Device{
		{ID: "1", Name: "otra cosa", Address: "1.1.1.1"},
		{ID: "2", Name: "OSN-COLLORES TOWER-OSNAP16-A", Address: "10.0.0.16"},
		{ID: "3", Name: "OSNAP16-A", Address: "10.0.0.99"},
		{ID: "4", Name: "CULEBRA NOC CAP2", Address: "10.8.8.8"},
	}
	d, ok := MatchDevice(devs, "OSNAP16-A")
	if !ok || d.Address != "10.0.0.99" {
		t.Fatalf("exact match: %+v ok=%t", d, ok)
	}
	d, ok = MatchDevice(devs, "CAP2")
	if !ok || d.Address != "10.8.8.8" {
		t.Fatalf("token match: %+v ok=%t", d, ok)
	}
	if _, ok := MatchDevice(devs, "NO-EXISTE"); ok {
		t.Fatal("no debía matchear")
	}
}
