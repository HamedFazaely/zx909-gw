package protocol

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Position holds a decoded location fix.
type Position struct {
	Time       time.Time
	Latitude   float64
	Longitude  float64
	SpeedKmh   float64
	Course     float64
	Satellites int
	Valid      bool
}

// ParseGPS decodes a classic GT06-style GPS body (used by 0x10 / 0x12 variants).
// Layout (common):
//   date/time 6 bytes (YY MM DD HH MM SS)
//   GPS info length + satellites 1 byte
//   latitude 4 bytes
//   longitude 4 bytes
//   speed 1 byte
//   course/status 2 bytes
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

	lat := float64(latRaw) / 1800000.0
	lon := float64(lonRaw) / 1800000.0

	// Status bits (common GT06):
	// bit 2: latitude N/S (0=N, 1=S)
	// bit 3: longitude E/W (0=E, 1=W)
	// bit 4: GPS valid
	if courseStatus&0x04 != 0 {
		lat = -lat
	}
	if courseStatus&0x08 != 0 {
		lon = -lon
	}
	valid := courseStatus&0x10 != 0
	course := float64(courseStatus & 0x03FF) // lower 10 bits often course

	return &Position{
		Time:       ts,
		Latitude:   lat,
		Longitude:  lon,
		SpeedKmh:   speed,
		Course:     course,
		Satellites: sats,
		Valid:      valid,
	}, nil
}
