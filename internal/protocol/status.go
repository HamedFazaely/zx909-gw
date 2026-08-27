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
// Classic GT06 / Concox (5-byte body after serial/CRC stripped):
//
//	terminal_info | voltage_level(0–6) | gsm(0–4) | alarm | language
type Status struct {
	BatteryPercent int  // 0–100, or -1 if unknown
	SoftwareVer    int  // raw version byte (Topin)
	Timezone       int  // signed hours offset from UTC (Topin)
	UploadInterval int  // minutes (Topin, best-effort)
	VoltageLevel   int  // classic 0–6, or -1
	GSMSignal      int  // classic 0–4, or -1
	Charging       bool // classic terminal-info bit 2
	GPSOn          bool // classic terminal-info bit 6
	ACC            bool // classic terminal-info bit 1
	OilCut         bool // classic terminal-info bit 7
	Armed          bool // classic terminal-info bit 0
	AlarmBits      int  // classic terminal-info bits 3–5
	Language       int  // classic language byte
	Classic        bool
	Raw            []byte
}

// voltageLevelPercent maps Concox voltage level 0–6 to a coarse percentage.
// The protocol does not send a real SOC; this is only for ThingsBoard charts.
func voltageLevelPercent(level int) int {
	switch level {
	case 0:
		return 0
	case 1:
		return 10
	case 2:
		return 20
	case 3:
		return 40
	case 4:
		return 60
	case 5:
		return 80
	case 6:
		return 100
	default:
		return -1
	}
}

// ParseStatus decodes a Topin / ZX909 0x13 body (battery percent first).
func ParseStatus(body []byte) (*Status, error) {
	if len(body) < 1 {
		return "", fmt.Errorf("status body empty")
	}

	s := &Status{
		BatteryPercent: -1,
		VoltageLevel:   -1,
		GSMSignal:      -1,
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
