package protocol

import (
	"encoding/hex"
	"testing"
)

func TestBuildACKMatchesPDFExample(t *testing.T) {
	ack := BuildACK(0x01, 0x0001)
	got := hex.EncodeToString(ack)
	want := "787805010001d9dc0d0a"
	if got != want {
		t.Fatalf("BuildACK = %s, want %s", got, want)
	}
}

func TestBuildLoginACK(t *testing.T) {
	ack := BuildLoginACK()
	got := hex.EncodeToString(ack)
	want := "787801010d0a"
	if got != want {
		t.Fatalf("BuildLoginACK = %s, want %s", got, want)
	}
}

func TestExtractRealLoginPacket(t *testing.T) {
	raw, _ := hex.DecodeString("78780d010861971080061526090d0a")
	f, n, err := ExtractFrame(raw)
	if err != nil {
		t.Fatalf("ExtractFrame: %v", err)
	}
	if n != len(raw) {
		t.Fatalf("consumed %d, want %d", n, len(raw))
	}
	if f.Proto != 0x01 {
		t.Fatalf("proto = %02X, want 01", f.Proto)
	}
	imei, err := ParseLogin(f.Body)
	if err != nil {
		t.Fatalf("ParseLogin: %v (body=%s)", err, hex.EncodeToString(f.Body))
	}
	if imei != "861971080061526" {
		t.Fatalf("IMEI = %s, want 861971080061526", imei)
	}
}

func TestExtractStatusPacket(t *testing.T) {
	raw, _ := hex.DecodeString("787816130a09030559000000000000000400121a080f022d3a0d0a")
	f, n, err := ExtractFrame(raw)
	if err != nil {
		t.Fatalf("ExtractFrame: %v", err)
	}
	if n != len(raw) {
		t.Fatalf("consumed %d", n)
	}
	if f.Proto != 0x13 {
		t.Fatalf("proto = %02X, want 13", f.Proto)
	}
}

func TestCRCIncludesLength(t *testing.T) {
	data := []byte{0x05, 0x01, 0x00, 0x01}
	if got := CRC16X25(data); got != 0xD9DC {
		t.Fatalf("CRC = %04X, want D9DC", got)
	}
}

func TestParseGPSSignBitsFromCapture(t *testing.T) {
	cases := []struct {
		name    string
		bodyHex string
		latMin  float64
		latMax  float64
		lonMin  float64
		lonMax  float64
		course  float64
	}{
		{
			name:    "first fix course 306",
			bodyHex: "1a080e1730089703d443e705813d661f1532fff5",
			latMin:  35.6, latMax: 35.8,
			lonMin:  51.2, lonMax: 51.4,
			course:  306,
		},
		{
			name:    "second fix course 316 (previously flipped south/west)",
			bodyHex: "1a080e1730129903d447780581385423153cfffb",
			latMin:  35.6, latMax: 35.8,
			lonMin:  51.2, lonMax: 51.4,
			course:  316,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := hex.DecodeString(tc.bodyHex)
			if err != nil {
				t.Fatal(err)
			}
			pos, err := ParseGPS(body)
			if err != nil {
				t.Fatal(err)
			}
			if pos.Latitude < tc.latMin || pos.Latitude > tc.latMax {
				t.Fatalf("lat=%.6f outside [%.1f,%.1f]", pos.Latitude, tc.latMin, tc.latMax)
			}
			if pos.Longitude < tc.lonMin || pos.Longitude > tc.lonMax {
				t.Fatalf("lon=%.6f outside [%.1f,%.1f]", pos.Longitude, tc.lonMin, tc.lonMax)
			}
			if pos.Course != tc.course {
				t.Fatalf("course=%.0f want %.0f", pos.Course, tc.course)
			}
			t.Log(pos.String())
		})
	}
}

func TestParseStatusShort(t *testing.T) {
	body, _ := hex.DecodeString("2d09032c4f")
	st, err := ParseStatus(body)
	if err != nil {
		t.Fatal(err)
	}
	if st.BatteryPercent != 45 {
		t.Fatalf("battery=%d want 45", st.BatteryPercent)
	}
	if st.SoftwareVer != 0x09 {
		t.Fatalf("sw=%d", st.SoftwareVer)
	}
	t.Log(st.String())
}
