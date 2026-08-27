package protocol

import (
	"encoding/hex"
	"testing"
)

func TestPromoteClassic_CapturedLogins(t *testing.T) {
	cases := []struct {
		raw    string
		serial uint16
		ack    string
	}{
		{"78780d0108680220306687300009ec0e0d0a", 0x0009, "78780501000955940d0a"},
		{"78780d010868022030668730000ade950d0a", 0x000A, "78780501000a670f0d0a"},
		{"78780d010868022030668730000bcf1c0d0a", 0x000B, "78780501000b76860d0a"},
	}
	for _, tc := range cases {
		raw, err := hex.DecodeString(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		f, n, err := ExtractFrame(raw)
		if err != nil {
			t.Fatalf("ExtractFrame %s: %v", tc.raw, err)
		}
		if n != len(raw) {
			t.Fatalf("consumed %d want %d", n, len(raw))
		}
		if !f.Classic {
			t.Fatalf("expected classic GT06 frame: body=%s", hex.EncodeToString(f.Body))
		}
		if f.Proto != MsgLogin || f.Serial != tc.serial {
			t.Fatalf("proto=%02X serial=%04X want 01/%04X", f.Proto, f.Serial, tc.serial)
		}
		imei, err := ParseLogin(f.Body)
		if err != nil {
			t.Fatal(err)
		}
		if imei != "868022030668730" {
			t.Fatalf("IMEI=%s", imei)
		}
		if len(f.Body) != 8 {
			t.Fatalf("classic login body should be IMEI only, got %d (%s)", len(f.Body), hex.EncodeToString(f.Body))
		}
		got := hex.EncodeToString(BuildACK(MsgLogin, f.Serial))
		if got != tc.ack {
			t.Fatalf("ACK=%s want %s", got, tc.ack)
		}
	}
}

func TestPromoteClassic_TopinLoginUntouched(t *testing.T) {
	// ZX909_EU / 365GPS login: dummy or short length, no CRC. Must not flip to classic.
	raw, _ := hex.DecodeString("78780d010861971080061526090d0a")
	f, n, err := ExtractFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(raw) {
		t.Fatalf("consumed %d", n)
	}
	if f.Classic {
		t.Fatalf("Topin login must not be treated as classic (serial=%04X body=%s)", f.Serial, hex.EncodeToString(f.Body))
	}
	imei, err := ParseLogin(f.Body)
	if err != nil {
		t.Fatal(err)
	}
	if imei != "861971080061526" {
		t.Fatalf("IMEI=%s", imei)
	}
}

func TestPromoteClassic_PDFHeartbeatExample(t *testing.T) {
	// PDF §5.4.3 server ACK example (incoming example CRC in that PDF is stale;
	// the documented server reply CRC is the one that must match Appendix A).
	ack := hex.EncodeToString(BuildACK(MsgStatus, 0x0011))
	if ack != "787805130011f9700d0a" {
		t.Fatalf("heartbeat ACK=%s want 787805130011f9700d0a", ack)
	}
}
