package protocol

import (
	"encoding/hex"
	"fmt"
)

// Status holds decoded fields from a 0x13 heartbeat / status packet.
//
// Short form observed on ZX909_EU (5-byte body):
//
//	battery% | sw_version | timezone | interval | ? 
//
// Long form (21-byte body) starts with the same prefix and appends
// extra status / terminal-info bytes we log as raw for now.
type Status struct {
	BatteryPercent int    // 0–100, or -1 if unknown
	SoftwareVer    int    // raw version byte
	Timezone       int    // signed hours offset from UTC (device-reported)
	UploadInterval int    // minutes (best-effort)
	Raw            []byte // full body for debugging
}

// ParseStatus decodes a 0x13 status / heartbeat body.
func ParseStatus(body []byte) (*Status, error) {
	if len(body) < 1 {
		return nil, fmt.Errorf("status body empty")
	}

	s := &Status{
		BatteryPercent: -1,
		Raw:            append([]byte(nil), body...),
	}

	// Battery: first byte is percentage on this firmware (0–100).
	if b := int(body[0]); b <= 100 {
		s.BatteryPercent = b
	}

	if len(body) >= 2 {
		s.SoftwareVer = int(body[1])
	}
	if len(body) >= 3 {
		// Timezone often stored as signed integer hours (e.g. 3 = UTC+3, or 0x08 = +8 on some firmwares)
		tz := int(body[2])
		if tz > 127 {
			tz -= 256
		}
		s.Timezone = tz
	}
	if len(body) >= 4 {
		s.UploadInterval = int(body[3])
	}

	return s, nil
}

// String returns a human-readable summary for logs.
func (s *Status) String() string {
	parts := fmt.Sprintf("battery=%d%% sw=0x%02X tz=%+d",
		s.BatteryPercent, s.SoftwareVer, s.Timezone)
	if s.UploadInterval > 0 {
		parts += fmt.Sprintf(" interval=%d", s.UploadInterval)
	}
	if len(s.Raw) > 0 {
		parts += " raw=" + hex.EncodeToString(s.Raw)
	}
	return parts
}
