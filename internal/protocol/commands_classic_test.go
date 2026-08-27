package protocol

import (
	"bytes"
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

func TestBuildClassicTimerAndHBT_ASCII(t *testing.T) {
	if !bytes.Contains(BuildClassicTimer(60, 180, 1), []byte("TIMER,60,180#")) {
		t.Fatal("TIMER,60,180# missing")
	}
	if !bytes.Contains(BuildClassicHeartbeat(180, 180, 1), []byte("HBT,180,180#")) {
		t.Fatal("HBT,180,180# missing")
	}
	if !bytes.Contains(BuildClassicStatusInterval(3, 1), []byte("HBT,180,180#")) {
		t.Fatal("status 3min should be HBT,180,180#")
	}
}
