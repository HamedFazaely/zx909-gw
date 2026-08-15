package protocol

import (
	"encoding/binary"
	"time"
)

// BuildTimeSyncReply answers 0x30 with GMT/UTC wall clock.
// Format from live 365gps traffic:
//
//	78 78 07 30 YYYY(2 BE) MM DD HH MM SS 0D 0A
func BuildTimeSyncReply(t time.Time) []byte {
	t = t.UTC()
	buf := make([]byte, 13)
	buf[0], buf[1] = 0x78, 0x78
	buf[2] = 0x07
	buf[3] = MsgTimeSync
	binary.BigEndian.PutUint16(buf[4:6], uint16(t.Year()))
	buf[6] = byte(t.Month())
	buf[7] = byte(t.Day())
	buf[8] = byte(t.Hour())
	buf[9] = byte(t.Minute())
	buf[10] = byte(t.Second())
	buf[11], buf[12] = 0x0D, 0x0A
	return buf
}

// BuildDefaultSettings is a minimal 0x57 settings blob based on observed
// vendor replies (upload interval, switches, empty SOS separators).
//
//	78 78 1F 57 00 10 01 7F 18 26 … 3B 3B 3B 0D 0A
func BuildDefaultSettings() []byte {
	// Captured from www.365gps.com during login handshake.
	return []byte{
		0x78, 0x78, 0x1F, 0x57,
		0x00, 0x10, // upload interval (BCD/hex as sent by vendor)
		0x01,       // switches
		0x7F, 0x18, 0x26, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // alarm block
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // dnd / gps timer
		0x00, 0x00, 0x00, 0x00, 0x00,
		0x3B, 0x3B, 0x3B, // SOS/Mom/Dad separators (empty)
		0x0D, 0x0A,
	}
}

// BuildRestart — 0x48 op=01
func BuildRestart() []byte {
	return []byte{0x78, 0x78, 0x02, MsgRestart, 0x01, 0x0D, 0x0A}
}

// BuildShutdown — 0x48 op=02
func BuildShutdown() []byte {
	return []byte{0x78, 0x78, 0x02, MsgRestart, 0x02, 0x0D, 0x0A}
}

// BuildUploadInterval sets location upload interval via 0x97 (seconds, 10–7200).
func BuildUploadInterval(seconds uint16) []byte {
	if seconds < 10 {
		seconds = 10
	}
	if seconds > 7200 {
		seconds = 7200
	}
	buf := []byte{0x78, 0x78, 0x03, MsgUploadInt, 0, 0, 0x0D, 0x0A}
	binary.BigEndian.PutUint16(buf[4:6], seconds)
	return buf
}

// BuildStatusInterval sets status packet interval via 0x13 (minutes).
func BuildStatusInterval(minutes byte) []byte {
	if minutes == 0 {
		minutes = 1
	}
	return []byte{0x78, 0x78, 0x02, MsgStatus, minutes, 0x0D, 0x0A}
}

// BuildLocate requests an immediate position upload (0x80).
// op 0x01 = GPS/WiFi/LBS, bare 0x80 = same family.
func BuildLocate() []byte {
	return []byte{0x78, 0x78, 0x01, 0x80, 0x0D, 0x0A}
}
