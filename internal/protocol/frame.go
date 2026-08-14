package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	ErrIncomplete   = errors.New("incomplete frame")
	ErrBadHeader    = errors.New("bad header")
	ErrBadTrailer   = errors.New("bad trailer")
	ErrBadCRC       = errors.New("CRC mismatch")
	ErrBadLength    = errors.New("invalid length")
)

const (
	HeaderShort = 0x7878
	HeaderExt   = 0x7979
	Trailer     = 0x0D0A
)

// Message type constants (GT06 / Topin family)
const (
	MsgLogin      byte = 0x01
	MsgGPS        byte = 0x10
	MsgGPSOffline byte = 0x11
	MsgGPSLBS     byte = 0x12
	MsgStatus     byte = 0x13
	MsgGPS2       byte = 0x22 // common on newer firmwares
	// Add more as we observe real ZX909 traffic
)

// Frame is a decoded GT06-style packet.
type Frame struct {
	Proto  byte
	Body   []byte
	Serial uint16
	Raw    []byte // full original bytes including header/trailer (for debugging)
}

// ExtractFrame tries to pull one complete frame from buf.
// Returns the frame, number of bytes consumed, and error.
func ExtractFrame(buf []byte) (*Frame, int, error) {
	if len(buf) < 5 {
		return nil, 0, ErrIncomplete
	}

	header := binary.BigEndian.Uint16(buf[0:2])
	var lengthFieldSize int
	var bodyLen int

	switch header {
	case HeaderShort:
		lengthFieldSize = 1
		bodyLen = int(buf[2])
	case HeaderExt:
		if len(buf) < 6 {
			return nil, 0, ErrIncomplete
		}
		lengthFieldSize = 2
		bodyLen = int(binary.BigEndian.Uint16(buf[2:4]))
	default:
		return nil, 0, ErrBadHeader
	}

	// bodyLen covers: protocol(1) + info content + serial(2) + crc(2)
	// Total frame size = 2 (header) + lengthFieldSize + bodyLen + 2 (trailer)
	total := 2 + lengthFieldSize + bodyLen + 2
	if len(buf) < total {
		return nil, 0, ErrIncomplete
	}

	if binary.BigEndian.Uint16(buf[total-2:total]) != Trailer {
		return nil, 0, ErrBadTrailer
	}

	startOfProto := 2 + lengthFieldSize
	proto := buf[startOfProto]
	// CRC is calculated over: protocol number + info content + serial
	crcData := buf[startOfProto : total-4] // up to but not including CRC + trailer
	expectedCRC := binary.BigEndian.Uint16(buf[total-4 : total-2])
	actualCRC := CRC16X25(crcData)
	if actualCRC != expectedCRC {
		return nil, 0, fmt.Errorf("%w: expected %04X got %04X", ErrBadCRC, expectedCRC, actualCRC)
	}

	serial := binary.BigEndian.Uint16(buf[total-6 : total-4])
	body := make([]byte, 0, bodyLen-5) // exclude proto+serial+crc
	if bodyLen > 5 {
		body = append(body, buf[startOfProto+1:total-6]...)
	}

	f := &Frame{
		Proto:  proto,
		Body:   body,
		Serial: serial,
		Raw:    append([]byte(nil), buf[:total]...),
	}
	return f, total, nil
}

// BuildACK builds a standard response for login / heartbeat style packets.
// Format: 78 78 05 PROTO SERIAL_H SERIAL_L CRC_H CRC_L 0D 0A
func BuildACK(proto byte, serial uint16) []byte {
	buf := make([]byte, 10)
	binary.BigEndian.PutUint16(buf[0:2], HeaderShort)
	buf[2] = 0x05 // length: proto + serial(2) + crc(2)
	buf[3] = proto
	binary.BigEndian.PutUint16(buf[4:6], serial)
	crc := CRC16X25(buf[3:6])
	binary.BigEndian.PutUint16(buf[6:8], crc)
	binary.BigEndian.PutUint16(buf[8:10], Trailer)
	return buf
}
