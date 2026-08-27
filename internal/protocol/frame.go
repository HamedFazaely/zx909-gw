package protocol

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

var (
	ErrIncomplete = errors.New("incomplete frame")
	ErrBadHeader  = errors.New("bad header")
	ErrBadTrailer = errors.New("bad trailer")
	ErrBadCRC     = errors.New("CRC mismatch")
	ErrBadLength  = errors.New("invalid length")
)

const (
	HeaderShort = 0x7878
	HeaderExt   = 0x7979
	Trailer     = 0x0D0A
)

// Message type constants (GT06 / Topin / ZX909 / 365GPS family)
const (
	MsgLogin       byte = 0x01
	MsgGPS         byte = 0x10
	MsgGPSOffline  byte = 0x11
	MsgGPSLBS      byte = 0x12
	MsgStatus      byte = 0x13
	MsgGPS2        byte = 0x22
	MsgWifiLBS     byte = 0x1A
	MsgWifiLBS2    byte = 0x1B
	MsgTimeSync    byte = 0x30
	MsgParam       byte = 0x57
	MsgICCID       byte = 0xB3
	MsgOfflineWifi byte = 0x17
	MsgOnlineWifi  byte = 0x69
	MsgRestart     byte = 0x48
	MsgUploadInt   byte = 0x97
	MsgAlarm       byte = 0x16
)

// Frame is a decoded packet.
type Frame struct {
	Proto   byte
	Body    []byte
	Serial  uint16
	Raw     []byte
	Classic bool // true when length+CRC match classic GT06 / Concox
}

// ExtractFrame pulls one complete frame from buf.
//
// Hybrid strategy (365GPS mid-body 0D0A warning):
//   - Login, status, GPS, time-sync: only accept trailer after a known minimum content length.
//   - Wi-Fi/LBS: walk datetime + optional LBS cell block when present; otherwise trailer after datetime.
//   - Unknown protos: legacy first-0D0A-after-header behaviour.
//
// Length bytes are not trusted (Topin often sends dummies).
// After a successful extract, promoteClassic upgrades frames whose length byte
// and CRC-ITU match classic GT06.
func ExtractFrame(buf []byte) (*Frame, int, error) {
	if len(buf) < 5 {
		return nil, 0, ErrIncomplete
	}

	start := -1
	for i := 0; i+1 < len(buf); i++ {
		if buf[i] == 0x78 && buf[i+1] == 0x78 {
			start = i
			break
		}
		if buf[i] == 0x79 && buf[i+1] == 0x79 {
			start = i
			break
		}
	}
	if start == -1 {
		return nil, len(buf), ErrBadHeader
	}
	if start > 0 {
		return nil, start, ErrBadHeader
	}

	header := binary.BigEndian.Uint16(buf[0:2])
	var (
		prefix int // bytes before content (header + len [+ ext] + proto)
		proto  byte
	)
	switch header {
	case HeaderShort:
		prefix = 4 // 78 78 LL PP
		if len(buf) < prefix {
			return nil, 0, ErrIncomplete
		}
		proto = buf[3]
	case HeaderExt:
		prefix = 5 // 79 79 LL LL PP (simplified: treat 2-byte len like existing code path)
		if len(buf) < 6 {
			return nil, 0, ErrIncomplete
		}
		// Keep compatibility with previous ExtractFrame: proto at index 4, content at 5.
		prefix = 5
		proto = buf[4]
	default:
		return nil, 2, ErrBadHeader
	}

	content := buf[prefix:]
	minContent, exact, known := contentBounds(proto, content)
	if known && exact >= 0 {
		// Layout gave an exact content length (e.g. complete LBS block).
		if len(content) < exact+2 {
			return nil, 0, ErrIncomplete
		}
		if content[exact] != 0x0D || content[exact+1] != 0x0A {
			// Exact end not trailed by 0D0A — fall back to search from exact.
			minContent = exact
		} else {
			end := prefix + exact + 2
			return frameFromRaw(buf[:end], header)
		}
	}
	if !known {
		minContent = 0
	}
	if len(content) < minContent+2 {
		return nil, 0, ErrIncomplete
	}

	relEnd := findTrailer(content, minContent)
	if relEnd < 0 {
		return nil, 0, ErrIncomplete
	}
	end := prefix + relEnd
	return frameFromRaw(buf[:end], header)
}

// contentBounds returns how to find the end of the content section (bytes after proto, before trailer).
//
//	min   — do not accept 0D0A before this many content bytes
//	exact — if >= 0, content length is known from layout (trailer should follow)
//	known — false means unknown proto (legacy scan from 0)
func contentBounds(proto byte, content []byte) (min int, exact int, known bool) {
	switch proto {
	case MsgLogin:
		return 8, -1, true // IMEI(8); classic GT06 then serial+CRC, Topin may add a version byte
	case MsgTimeSync:
		return 0, -1, true // request is empty content; reply is handled as S→C
	case MsgStatus:
		return 4, -1, true // battery, sw, tz, interval (+ optional signal)
	case MsgGPS, MsgGPSOffline, MsgGPSLBS, MsgGPS2, MsgAlarm:
		return 18, -1, true // DT(6)+info(1)+lat(4)+lon(4)+speed(1)+course(2); altitude optional after
	case MsgWifiLBS, MsgWifiLBS2, MsgOfflineWifi, MsgOnlineWifi:
		return wifiLBSContentBounds(content)
	case MsgICCID:
		return 1, -1, true
	case MsgParam:
		return 0, -1, true
	default:
		return 0, -1, false
	}
}

// wifiLBSContentBounds implements layout-aware bounds for 0x1A/0x1B/0x17/0x69.
// Datetime is 6 bytes; Wi-Fi is N×7; LBS is count+MCC+MNC+cells when present.
func wifiLBSContentBounds(content []byte) (min int, exact int, known bool) {
	known = true
	min = 6
	exact = -1
	if len(content) < 6 {
		return min, exact, known
	}
	rest := content[6:]
	lbsStart := findLBSHeader(rest)
	if lbsStart < 0 {
		// Wi-Fi only (or LBS not yet in buffer): trailer search from after datetime.
		return min, exact, known
	}
	lbs := rest[lbsStart:]
	if len(lbs) < 4 {
		return min, exact, known // incomplete LBS header
	}
	count := int(lbs[0])
	if count < 1 || count > 6 {
		return min, exact, known
	}
	pos := 4
	cellSize := 7
	if pos+count*7 > len(lbs) && pos+count*6 <= len(lbs) {
		cellSize = 6
	}
	need := pos + count*cellSize
	if len(lbs) < need {
		// Cells not fully present yet — wait for more data (do not cut on early 0D0A).
		min = 6 + lbsStart + need
		return min, exact, known
	}
	exact = 6 + lbsStart + need
	return min, exact, known
}

// findTrailer returns the end offset (exclusive) within content of the first
// 0D0A at or after minContent. Returns -1 if not found.
func findTrailer(content []byte, minContent int) int {
	if minContent < 0 {
		minContent = 0
	}
	for i := minContent; i+1 < len(content); i++ {
		if content[i] == 0x0D && content[i+1] == 0x0A {
			return i + 2
		}
	}
	return -1
}

func frameFromRaw(raw []byte, header uint16) (*Frame, int, error) {
	end := len(raw)
	if end < 5 {
		return nil, end, ErrBadLength
	}
	var proto byte
	var body []byte
	var serial uint16
	switch header {
	case HeaderShort:
		proto = raw[3]
		body, serial = splitBodyAndSerial(raw[4 : end-2])
	case HeaderExt:
		if end < 6 {
			return nil, end, ErrBadLength
		}
		proto = raw[4]
		body, serial = splitBodyAndSerial(raw[5 : end-2])
	default:
		return nil, end, ErrBadHeader
	}
	f := &Frame{
		Proto:  proto,
		Body:   body,
		Serial: serial,
		Raw:    append([]byte(nil), raw...),
	}
	promoteClassic(f)
	return f, end, nil
}

func splitBodyAndSerial(content []byte) ([]byte, uint16) {
	return append([]byte(nil), content...), 0
}

// BuildACK classic GT06-style (kept for tests / PDF compatibility).
func BuildACK(proto byte, serial uint16) []byte {
	buf := make([]byte, 10)
	binary.BigEndian.PutUint16(buf[0:2], HeaderShort)
	buf[2] = 0x05
	buf[3] = proto
	binary.BigEndian.PutUint16(buf[4:6], serial)
	crc := CRC16X25(buf[2:6])
	binary.BigEndian.PutUint16(buf[6:8], crc)
	binary.BigEndian.PutUint16(buf[8:10], Trailer)
	return buf
}

// BuildLoginACK — 365GPS login success reply.
//
//	78 78 01 01 0D 0A
func BuildLoginACK() []byte {
	return []byte{0x78, 0x78, 0x01, 0x01, 0x0D, 0x0A}
}

// BuildSimpleACK — minimal proto-only ACK (legacy / non-must-reply protos).
//
//	78 78 01 PROTO 0D 0A
func BuildSimpleACK(proto byte) []byte {
	return []byte{0x78, 0x78, 0x01, proto, 0x0D, 0x0A}
}

// BuildDatetimeACK is the 365GPS must-reply form for GPS and Wi-Fi/LBS packets:
//
//	78 78 00 PROTO YY MM DD HH MM SS 0D 0A
//
// Length byte is dummy 0x00 (per doc). datetime is the 6 bytes from the
// device packet (binary for 0x10/0x11, BCD for 0x1A/0x1B/0x69 — echo as-is).
func BuildDatetimeACK(proto byte, datetime []byte) []byte {
	if len(datetime) < 6 {
		datetime = []byte{0, 0, 0, 0, 0, 0}
	}
	buf := make([]byte, 12)
	buf[0], buf[1] = 0x78, 0x78
	buf[2] = 0x00 // dummy length per 365GPS doc
	buf[3] = proto
	copy(buf[4:10], datetime[:6])
	buf[10], buf[11] = 0x0D, 0x0A
	return buf
}

// BuildStatusEcho replies to 0x13 by echoing the received frame (365GPS doc).
// If raw is empty, falls back to a minimal status ACK.
func BuildStatusEcho(raw []byte) []byte {
	if len(raw) >= 5 {
		return append([]byte(nil), raw...)
	}
	return BuildSimpleACK(MsgStatus)
}

// DatetimeFromBody returns the first 6 body bytes for ACK echoing.
func DatetimeFromBody(body []byte) []byte {
	if len(body) >= 6 {
		return body[:6]
	}
	return []byte{0, 0, 0, 0, 0, 0}
}

func (f *Frame) Hex() string {
	return hex.EncodeToString(f.Raw)
}

func (f *Frame) String() string {
	if f.Classic {
		return fmt.Sprintf("proto=0x%02X serial=%04X body_len=%d classic raw=%s", f.Proto, f.Serial, len(f.Body), f.Hex())
	}
	return fmt.Sprintf("proto=0x%02X body_len=%d raw=%s", f.Proto, len(f.Body), f.Hex())
}

// ProtoHex returns "0xNN" for structured logs.
func ProtoHex(p byte) string {
	return fmt.Sprintf("0x%02X", p)
}
