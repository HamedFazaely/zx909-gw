package protocol

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Position holds a decoded location fix.
type Position struct {
	Time         time.Time
	Latitude     float64
	Longitude    float64
	SpeedKmh     float64
	Course       float64
	Satellites   int
	Valid        bool
	CourseStatus uint16 // raw flags — logged for reverse-engineering
}

// ParseGPS decodes a GT06 / Topin / ZX909 GPS body (0x10 / 0x11 / 0x12 / 0x22).
//
// Layout:
//
//	date/time      6 bytes (YY MM DD HH MM SS, UTC)
//	GPS info       1 byte  (low nibble = satellite count)
//	latitude       4 bytes (raw / 1_800_000 → degrees, absolute)
//	longitude      4 bytes (raw / 1_800_000 → degrees, absolute)
//	speed          1 byte  (km/h)
//	course/status  2 bytes (low 10 bits = course; upper bits = flags)
//
// Hemisphere note (ZX909_EU):
// Classic GT06 docs put N/S in bit 2 and E/W in bit 3 of course/status.
// Live captures showed those bits track the course value and incorrectly
// flipped Tehran fixes to the southern/western hemisphere. High bits
// (12/13) are also inconsistent with expected polarity on this firmware.
// Until we observe a genuine Southern/Western fix we keep the absolute
// values from the raw fields (correct for all captures so far).
func ParseGPS(body []byte) (*Position, error) {
	if len(body) < 18 {
		return nil, fmt.Errorf("GPS body too short: %d", len(body))
	}

	year := 2000 + int(body[0])
	month := time.Month(body[1])
	day := int(body[2])
	hour := int(body[3])
	min := int(body[4])
	sec := int(body[5])
	ts := time.Date(year, month, day, hour, min, sec, 0, time.UTC)

	gpsInfo := body[6]
	sats := int(gpsInfo & 0x0F)

	latRaw := binary.BigEndian.Uint32(body[7:11])
	lonRaw := binary.BigEndian.Uint32(body[11:15])
	speed := float64(body[15])
	courseStatus := binary.BigEndian.Uint16(body[16:18])

	lat := float64(latRaw) / 1_800_000.0
	lon := float64(lonRaw) / 1_800_000.0

	course := float64(courseStatus & 0x03FF)
	// Valid: bit 4 (classic) or bit 11 (some Topin) — treat as valid if either set,
	// or if we have a non-zero coordinate (device only sends 0x11 when it has a fix).
	valid := courseStatus&0x0010 != 0 || courseStatus&0x0800 != 0 || (lat != 0 && lon != 0)

	return &Position{
		Time:         ts,
		Latitude:     lat,
		Longitude:    lon,
		SpeedKmh:     speed,
		Course:       course,
		Satellites:   sats,
		Valid:        valid,
		CourseStatus: courseStatus,
	}, nil
}

// String returns a human-readable summary for logs.
func (p *Position) String() string {
	return fmt.Sprintf("time=%s lat=%.6f lon=%.6f speed=%.0fkm/h course=%.0f° sats=%d valid=%v flags=0x%04X",
		p.Time.Format("2006-01-02 15:04:05 UTC"),
		p.Latitude, p.Longitude, p.SpeedKmh, p.Course, p.Satellites, p.Valid, p.CourseStatus)
}
