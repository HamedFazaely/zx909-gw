package protocol

import (
	"encoding/hex"
	"testing"
)

func TestBuildACKMatchesPDFExample(t *testing.T) {
	// From GT06 doc §5.1.2: length 05, proto 01, serial 0001 → CRC D9 DC
	ack := BuildACK(0x01, 0x0001)
	got := hex.EncodeToString(ack)
	want := "787805010001d9dc0d0a"
	if got != want {
		t.Fatalf("BuildACK = %s, want %s", got, want)
	}
}

func TestExtractFrameRoundTrip(t *testing.T) {
	ack := BuildACK(0x13, 0x00AB)
	f, n, err := ExtractFrame(ack)
	if err != nil {
		t.Fatalf("ExtractFrame: %v", err)
	}
	if n != len(ack) {
		t.Fatalf("consumed %d, want %d", n, len(ack))
	}
	if f.Proto != 0x13 || f.Serial != 0x00AB {
		t.Fatalf("proto/serial = %02X/%04X", f.Proto, f.Serial)
	}
}

func TestCRCIncludesLength(t *testing.T) {
	// Known vector from PDF
	data := []byte{0x05, 0x01, 0x00, 0x01}
	if got := CRC16X25(data); got != 0xD9DC {
		t.Fatalf("CRC = %04X, want D9DC", got)
	}
}
