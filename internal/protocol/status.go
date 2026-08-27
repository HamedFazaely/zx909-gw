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
//	terminal_info | voltage_level(0-6) | gsm(0-4) | alarm | language
type Status struct {
	BatteryPercent int
	SoftwareVer    int
	Timezone       int
	UploadInterval int
	VoltageLevel   int
	GSMSignal      int
	Charging       bool
	GPSOn          bool
	ACC            bool
	OilCut         bool
	Armed          bool
	AlarmBits      int
	Language       int
	Classic        bool
	Raw            []byte
}

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
		return nil, fmt.Errorf("status body empty")
	}

	s := &Status{
		BatteryPercent: -1,
		VoltageLevel:   -1,
		GSMSignal:      -1,
		Raw:            append([]byte(nil), body...),
	}

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

// ParseClassicStatus decodes a Concox / GT06 0x13 information content.
//
//	[0] terminal information bits
//	[1] voltage level 0-6
//	[2] GSM signal 0-4
//	[3] alarm
//	[4] language (0x01 Chinese, 0x02 English)
func ParseClassicStatus(body []byte) (*Status, error) {
	if len(body) < 3 {
		return nil, fmt.Errorf("classic status body too short: %d", len(body))
	}

	info := body[0]
	level := int(body[1])
	gsm := int(body[2])

	s := &Status{
		Classic:        true,
		BatteryPercent: voltageLevelPercent(level),
		VoltageLevel:   level,
		GSMSignal:      gsm,
		OilCut:         info&0x80 != 0,
		GPSOn:          info&0x40 != 0,
		AlarmBits:      int((info >> 3) & 0x07),
		Charging:       info&0x04 != 0,
		ACC:            info&0x02 != 0,
		Armed:          info&0x01 != 0,
		Raw:            append([]byte(nil), body...),
	}
	if len(body) >= 5 {
		s.Language = int(body[4])
	}
	return s, nil
}

// DecodeStatus picks Topin vs classic GT06 heartbeat decoding.
func DecodeStatus(classic bool, body []byte) (*Status, error) {
	if classic {
		return ParseClassicStatus(body)
	}
	return ParseStatus(body)
}

// Telemetry is the ThingsBoard attribute set for this heartbeat.
func (s *Status) Telemetry() map[string]any {
	values := map[string]any{}
	if s.BatteryPercent >= 0 {
		values["battery"] = s.BatteryPercent
	}
	if s.Classic {
		values["voltage_level"] = s.VoltageLevel
		values["gsm_signal"] = s.GSMSignal
		values["charging"] = s.Charging
		values["gps_on"] = s.GPSOn
		values["acc"] = s.ACC
	}
	return values
}

func (s *Status) String() string {
	if s.Classic {
		parts := fmt.Sprintf("battery~%d%% volt_lvl=%d gsm=%d charge=%v gps=%v acc=%v armed=%v oil_cut=%v lang=%d",
			s.BatteryPercent, s.VoltageLevel, s.GSMSignal, s.Charging, s.GPSOn, s.ACC, s.Armed, s.OilCut, s.Language)
		if len(s.Raw) > 0 {
			parts += " raw=" + hex.EncodeToString(s.Raw)
		}
		return parts
	}
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
