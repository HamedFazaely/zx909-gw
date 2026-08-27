package protocol

import (
	"encoding/hex"
	"testing"
)

func TestParseClassicString_LocateMapsURL(t *testing.T) {
	body, err := hex.DecodeString("44000000013c30382d32372031323a31363e687474703a2f2f6d6170732e676f6f676c652e636f6d2f6d6170733f713d2b33352e3638363238382c2b35312e3536333938320001")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParseClassicString(body)
	if err != nil {
		t.Fatal(err)
	}
	if info.Flag != 1 {
		t.Fatalf("flag %d", info.Flag)
	}
	if !info.HasPos {
		t.Fatalf("expected coords in %q", info.Text)
	}
	if abs(info.Lat-35.686288) > 1e-6 || abs(info.Lon-51.563982) > 1e-6 {
		t.Fatalf("coords %f %f", info.Lat, info.Lon)
	}
	if info.Time.Month() != 8 || info.Time.Day() != 27 || info.Time.Hour() != 12 || info.Time.Minute() != 16 {
		t.Fatalf("stamp %v", info.Time)
	}
}

func TestParseClassicString_LatLonText(t *testing.T) {
	text := "Lat:N35.480960,Lon:E51.401542"
	body := append([]byte{byte(4 + len(text)), 0, 0, 0, 1}, text...)
	info, err := ParseClassicString(body)
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasPos || abs(info.Lat-35.480960) > 1e-6 || abs(info.Lon-51.401542) > 1e-6 {
		t.Fatalf("%v", info)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
