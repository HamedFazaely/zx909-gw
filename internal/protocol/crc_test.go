package protocol

import (
	"encoding/hex"
	"testing"
)

func TestBuildACKMatchesPDFExample(t *testing.T) {
	// Classic GT06 doc example still valid for BuildACK
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
	// Real ZX909_EU login captured via MITM
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
