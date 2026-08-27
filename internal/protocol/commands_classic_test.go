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

func TestBuildClassicCommand_WHERE_TIMER_HBT(t *testing.T) {
	cases := []struct {
		got  []byte
		want string
	}{
		{BuildClassicLocate(1), "787810800a000000015748455245230001389f0d0a"},
		{BuildClassicUploadInterval(60, 1), "787813800d0000000154494d45522c3630230001a9cd0d0a"},
		{BuildClassicStatusInterval(2, 1), "787812800c000000014842542c313230230001e7af0d0a"},
		{BuildClassicShutdown(1), "787813800d00000001504f5745524f46462300018b0e0d0a"},
	}
	for _, tc := range cases {
		if hex.EncodeToString(tc.got) != tc.want {
			t.Fatalf("got %s want %s", hex.EncodeToString(tc.got), tc.want)
		}
	}
}
