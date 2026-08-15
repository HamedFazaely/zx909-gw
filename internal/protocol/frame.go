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

// Message type constants (GT06 / Topin / ZX909 family)
const (
	MsgLogin      byte = 0x01
	MsgGPS        byte = 0x10
	MsgGPSOffline byte = 0x11
	MsgGPSLBS     byte = 0x12
	MsgStatus     byte = 0x13
	MsgGPS2       byte = 0x22
	MsgWifiLBS    byte = 0x1A // observed on ZX909_EU (Wi-Fi + LBS)
	MsgWifiLBS2   byte = 0x1B // observed on ZX909_EU (Wi-Fi + LBS, variant)
	MsgTimeSync   byte = 0x30 // observed
	MsgParam      byte = 0x57 // observed
	MsgICCID      byte = 0xB3 // observed
	// More will be added as we capture traffic
)

// Frame is a decoded packet (Topin/ZX909 or classic GT06).
type Frame struct {
	Proto  byte
	Body   []byte
	Serial uint16 // 0 when the packet has no serial (common on this firmware)
	Raw    []byte // full original bytes including header/trailer
}

// ExtractFrame pulls one complete frame from buf.
//
// ZX909_EU / Topin firmwares do NOT always follow classic GT06 length+CRC rules.
// Real traffic uses short packets without CRC (login, many status/control messages).
// We therefore use a trailer-based extractor (78 78 … 0D 0A) which matches the
// working Python MITM the device already talks to.
func ExtractFrame(buf []byte) (*Frame, int, error) {
	if len(buf) < 5 {
		return nil, 0, ErrIncomplete
	}

	// Locate start bit
	start := -1
	for i := 0; i+1 < len(buf); i++ {
		if buf[i] == 0x78 && buf[i+1] == 0x78 {
			start = i
			break
		}
		// also accept extended header 79 79
		if buf[i] == 0x79 && buf[i+1] == 0x79 {
			start = i
			break
		}
	}
	if start == -1 {
		// nothing that looks like a header — drop everything so the caller can resync
		return nil, len(buf), ErrBadHeader
	}
	if start > 0 {
		// garbage before header; caller should discard it
		return nil, start, ErrBadHeader
	}

	// Locate trailer 0D 0A after the header
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

	// Short header (78 78) is what we see on this device
	header := binary.BigEndian.Uint16(raw[0:2])
	var proto byte
	var body []byte
	var serial uint16

	switch header {
	case HeaderShort:
		// layout: 78 78 | len(1) | proto(1) | content… | [serial?] [crc?] | 0D 0A
		if len(raw) < 5 {
			return nil, end, ErrBadLength
		}
		proto = raw[3]
		// Everything between proto and trailer is "body" for our purposes.
		// Classic packets put serial+crc at the end of this region; short Topin
		// packets often have neither.
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

	f := &Frame{
		Proto:  proto,
		Body:   body,
		Serial: serial,
		Raw:    raw,
	}
	return f, end, nil
}

// splitBodyAndSerial tries to peel a trailing 2-byte serial off the content
// when the packet looks long enough to be classic GT06 style. For short
// Topin packets it just returns the whole content and serial=0.
func splitBodyAndSerial(content []byte) ([]byte, uint16) {
	if len(content) >= 4 {
		// Heuristic: if the last 4 bytes look like serial(2)+crc(2) we could
		// strip them, but many Topin packets put useful data there.
		// For now keep the whole content as Body; serial stays 0 unless we
		// later add stricter classic parsing.
		return append([]byte(nil), content...), 0
	}
	return append([]byte(nil), content...), 0
}

// BuildACK builds a classic GT06-style ACK (length 05 + serial + CRC).
// Prefer BuildLoginACK / BuildSimpleACK for ZX909_EU which expects shorter replies.
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

// BuildLoginACK is the short ACK that 365gps.com sends for login on this firmware:
//   78 78 01 01 0D 0A
func BuildLoginACK() []byte {
	return []byte{0x78, 0x78, 0x01, 0x01, 0x0D, 0x0A}
}

// BuildSimpleACK is a minimal proto-only ACK used by the vendor for several message types.
// Format: 78 78 01 PROTO 0D 0A
func BuildSimpleACK(proto byte) []byte {
	return []byte{0x78, 0x78, 0x01, proto, 0x0D, 0x0A}
}

// Hex returns the raw frame as a hex string (handy for logs).
func (f *Frame) Hex() string {
	return hex.EncodeToString(f.Raw)
}

// String implements fmt.Stringer for debug logs.
func (f *Frame) String() string {
	return fmt.Sprintf("proto=0x%02X serial=%04X body_len=%d raw=%s",
		f.Proto, f.Serial, len(f.Body), f.Hex())
}
