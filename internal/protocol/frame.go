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
	MsgLogin      byte = 0x01
	MsgGPS        byte = 0x10
	MsgGPSOffline byte = 0x11
	MsgGPSLBS     byte = 0x12
	MsgStatus     byte = 0x13
	MsgGPS2       byte = 0x22
	MsgWifiLBS    byte = 0x1A
	MsgWifiLBS2   byte = 0x1B
	MsgTimeSync   byte = 0x30
	MsgParam      byte = 0x57
	MsgICCID      byte = 0xB3
	MsgOfflineWifi byte = 0x17
	MsgOnlineWifi  byte = 0x69
	MsgRestart     byte = 0x48
	MsgUploadInt   byte = 0x97
)

// Frame is a decoded packet.
type Frame struct {
	Proto  byte
	Body   []byte
	Serial uint16
	Raw    []byte
}

// ExtractFrame pulls one complete frame from buf using trailer-based framing
// (78 78 … 0D 0A), matching 365GPS / ZX909_EU live traffic.
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

	end := -1
	for i := 2; i+1 < len(buf); i++ {
		if buf[i] == 0x0D && buf[i+1] == 0x0A {
			end = i + 2
			break
		}
	}
	if end == -1 {
		return nil, 0, ErrIncomplete
	}

	raw := append([]byte(nil), buf[:end]...)
	if len(raw) < 5 {
		return nil, end, ErrBadLength
	}

	header := binary.BigEndian.Uint16(raw[0:2])
	var proto byte
	var body []byte
	var serial uint16

	switch header {
	case HeaderShort:
		if len(raw) < 5 {
			return nil, end, ErrBadLength
		}
		proto = raw[3]
		content := raw[4 : len(raw)-2]
		body, serial = splitBodyAndSerial(content)
	case HeaderExt:
		if len(raw) < 6 {
			return nil, end, ErrBadLength
		}
		proto = raw[4]
		content := raw[5 : len(raw)-2]
		body, serial = splitBodyAndSerial(content)
	default:
		return nil, end, ErrBadHeader
	}

	return &Frame{Proto: proto, Body: body, Serial: serial, Raw: raw}, end, nil
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
//   78 78 01 01 0D 0A
func BuildLoginACK() []byte {
	return []byte{0x78, 0x78, 0x01, 0x01, 0x0D, 0x0A}
}

// BuildSimpleACK — minimal proto-only ACK (legacy / non-must-reply protos).
//   78 78 01 PROTO 0D 0A
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
	return fmt.Sprintf("proto=0x%02X body_len=%d raw=%s", f.Proto, len(f.Body), f.Hex())
}

// ProtoHex returns "0xNN" for structured logs.
func ProtoHex(p byte) string {
	return fmt.Sprintf("0x%02X", p)
}
