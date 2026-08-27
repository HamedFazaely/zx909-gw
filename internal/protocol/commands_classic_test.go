package protocol

import (
	"encoding/hex"
	"testing"
)

func TestBuildClassicCommand_PDFExampleDYD(t *testing.T) {
	raw, _ := hex.DecodeString("787815800F000019014459442C3030303030302300A086980D0A")
	declared := int(raw[2])
	crcData := raw[2 : 1+declared]
	got := uint16(raw[1+declared])<<8 | uint16(raw[2+declared])
	if CRC16X25(crcData) != got {
		t.Fatalf("example CRC calc=%04X want %04X", CRC16X25(crcData), got)
	}
}

func TestBuildClassicCommand_RESET(t *testing.T) {
	pkt := BuildClassicRestart(1)
	got := hex.EncodeToString(pkt)
	want := "787810800a000000015245534554230001807d0d0a"
	if got != want {
		t.Fatalf("RESET# = %s want %s", got, want)
	}
}

func TestBuildClassicTimer_Live60_180(t *testing.T) {
	// Captured send of TIMER,60,180# to 868022030668730 (serial 1 in builder).
	got := hex.EncodeToString(BuildClassicTimer(60, 180, 1))
	want := "78781780110000000154494d45522c36302c313830230001b6890d0a"
	if got != want {
		t.Fatalf("TIMER,60,180# = %s want %s", got, want)
	}
}
