package protocol

import (
	"encoding/hex"
	"testing"
)

func TestParseClassicStatus_CapturedHeartbeat(t *testing.T) {
	body, _ := hex.DecodeString("0006040001")
	st, err := ParseClassicStatus(body)
	if err != nil {
		t.Fatal(err)
	}
	if st.VoltageLevel != 6 {
		t.Fatalf("voltage level=%d want 6 (Very High)", st.VoltageLevel)
	}
	if st.GSMSignal != 4 {
		t.Fatalf("gsm=%d want 4 (strong)", st.GSMSignal)
	}
	if st.BatteryPercent != 100 {
		t.Fatalf("mapped battery=%d want 100", st.BatteryPercent)
	}
	if st.Charging || st.ACC || st.Armed || st.OilCut || st.GPSOn {
		t.Fatalf("unexpected flags: %+v", st)
	}
	if st.Language != 1 {
		t.Fatalf("language=%d want 1", st.Language)
	}
}

func TestParseStatus_TopinStillPercent(t *testing.T) {
	body, _ := hex.DecodeString("2d09032c4f")
	st, err := ParseStatus(body)
	if err != nil {
		t.Fatal(err)
	}
	if st.Classic || st.BatteryPercent != 45 {
		t.Fatalf("topin battery=%d classic=%v", st.BatteryPercent, st.Classic)
	}
}
