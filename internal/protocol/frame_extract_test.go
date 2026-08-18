package protocol

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

func TestExtractFrame_Login(t *testing.T) {
	// 78 78 0A 01 + IMEI 8 BCD + ver + 0D 0A
	raw := mustDecode(t, "78780a010861971080061526390d0a")
	f, n, err := ExtractFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(raw) || f.Proto != MsgLogin || len(f.Body) != 9 {
		t.Fatalf("proto=%02x body=%d n=%d", f.Proto, len(f.Body), n)
	}
}

func TestExtractFrame_TimeSyncRequest(t *testing.T) {
	raw := []byte{0x78, 0x78, 0x01, 0x30, 0x0D, 0x0A}
	f, n, err := ExtractFrame(raw)
	if err != nil || n != 6 || f.Proto != MsgTimeSync || len(f.Body) != 0 {
		t.Fatalf("err=%v n=%d proto=%02x body=%d", err, n, f.Proto, len(f.Body))
	}
}

func TestExtractFrame_Status(t *testing.T) {
	raw := mustDecode(t, "78780613643903020d0a") // battery 100, sw, tz, interval
	f, n, err := ExtractFrame(raw)
	if err != nil || f.Proto != MsgStatus || n != len(raw) {
		t.Fatalf("err=%v proto=%02x n=%d", err, f.Proto, n)
	}
}

func TestExtractFrame_GPS(t *testing.T) {
	body := mustDecode(t, "0a03170f32179c026b3f3e0c22ad651f3460")
	if len(body) != 18 {
		t.Fatalf("body len %d", len(body))
	}
	raw := append([]byte{0x78, 0x78, 0x12, MsgGPS}, body...)
	raw = append(raw, 0x0D, 0x0A)
	f, n, err := ExtractFrame(raw)
	if err != nil || f.Proto != MsgGPS || n != len(raw) || len(f.Body) != 18 {
		t.Fatalf("err=%v proto=%02x n=%d body=%d", err, f.Proto, n, len(f.Body))
	}
}

// Mid-body 0D0A inside GPS datetime must not terminate the frame.
func TestExtractFrame_GPS_EmbeddedCRLFInDatetime(t *testing.T) {
	body := mustDecode(t, "0d0a010c1e009c026b3f3e0c22ad651f3460")
	if len(body) != 18 {
		t.Fatalf("body len %d", len(body))
	}
	raw := append([]byte{0x78, 0x78, 0x12, MsgGPS}, body...)
	raw = append(raw, 0x0D, 0x0A)

	f, n, err := ExtractFrame(raw)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if n != len(raw) {
		t.Fatalf("consumed %d want %d (cut early on mid-body 0D0A?)", n, len(raw))
	}
	if f.Proto != MsgGPS || len(f.Body) != 18 {
		t.Fatalf("proto=%02x body=%d", f.Proto, len(f.Body))
	}
	if !bytes.Equal(f.Body[:2], []byte{0x0D, 0x0A}) {
		t.Fatalf("datetime prefix lost: %x", f.Body[:2])
	}
}

func TestExtractFrame_Status_WithSignal(t *testing.T) {
	raw := mustDecode(t, "7878071364390302640d0a")
	f, n, err := ExtractFrame(raw)
	if err != nil || n != len(raw) || f.Proto != MsgStatus {
		t.Fatalf("err=%v n=%d proto=%02x", err, n, f.Proto)
	}
	if len(f.Body) != 5 {
		t.Fatalf("body %d", len(f.Body))
	}
}

func TestExtractFrame_Status_EmbeddedCRLFInFirstFields(t *testing.T) {
	// content: 0D 0A 08 03 — first two bytes look like trailer but min=4 blocks early cut
	raw := append([]byte{0x78, 0x78, 0x06, MsgStatus, 0x0D, 0x0A, 0x08, 0x03}, 0x0D, 0x0A)
	f, n, err := ExtractFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(raw) {
		t.Fatalf("consumed %d want %d", n, len(raw))
	}
	if len(f.Body) != 4 || f.Body[0] != 0x0D || f.Body[1] != 0x0A {
		t.Fatalf("body=%x", f.Body)
	}
}

func TestExtractFrame_WifiLBS_WithCells(t *testing.T) {
	content := []byte{
		0x17, 0x06, 0x22, 0x12, 0x30, 0x31, // datetime
		0x01,       // cell count
		0x01, 0xB0, // MCC 432
		0x0B,       // MNC 11
		0x12, 0x34, // LAC
		0x00, 0x00, 0x56, 0x78, // Cell ID
		0x40, // signal
	}
	raw := append([]byte{0x78, 0x78, 0x00, MsgWifiLBS}, content...)
	raw = append(raw, 0x0D, 0x0A)

	f, n, err := ExtractFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(raw) || f.Proto != MsgWifiLBS {
		t.Fatalf("n=%d proto=%02x", n, f.Proto)
	}
	wl, err := ParseWifiLBS(f.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(wl.Cells) != 1 || wl.Cells[0].MCC != 432 {
		t.Fatalf("cells=%+v", wl.Cells)
	}
}

func TestExtractFrame_WifiLBS_EmbeddedCRLFInDatetime(t *testing.T) {
	content := []byte{
		0x0D, 0x0A, 0x22, 0x12, 0x30, 0x31,
		0x01,
		0x01, 0xB0,
		0x0B,
		0x12, 0x34,
		0x00, 0x00, 0x56, 0x78,
		0x40,
	}
	raw := append([]byte{0x78, 0x78, 0x00, MsgWifiLBS}, content...)
	raw = append(raw, 0x0D, 0x0A)

	f, n, err := ExtractFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(raw) {
		t.Fatalf("consumed %d want %d", n, len(raw))
	}
	if f.Proto != MsgWifiLBS {
		t.Fatalf("proto %02x", f.Proto)
	}
}

func TestExtractFrame_Incomplete(t *testing.T) {
	partial := []byte{0x78, 0x78, 0x12, MsgGPS, 0x0A, 0x03}
	_, _, err := ExtractFrame(partial)
	if err != ErrIncomplete {
		t.Fatalf("got %v", err)
	}
}

func TestExtractFrame_TwoFrames(t *testing.T) {
	a := []byte{0x78, 0x78, 0x01, 0x30, 0x0D, 0x0A}
	b := mustDecode(t, "78780613643903020d0a")
	buf := append(append([]byte{}, a...), b...)
	f1, n1, err := ExtractFrame(buf)
	if err != nil || f1.Proto != MsgTimeSync || n1 != len(a) {
		t.Fatalf("first: err=%v proto=%02x n=%d", err, f1.Proto, n1)
	}
	f2, n2, err := ExtractFrame(buf[n1:])
	if err != nil || f2.Proto != MsgStatus || n2 != len(b) {
		t.Fatalf("second: err=%v proto=%02x n=%d", err, f2.Proto, n2)
	}
}
