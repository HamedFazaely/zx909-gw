package protocol

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// StringInfo is a classic GT06 0x15 command reply (PDF string information).
type StringInfo struct {
	Flag     uint32
	Text     string
	Language uint16
	Time     time.Time
	Lat      float64
	Lon      float64
	HasPos   bool
}

var (
	reMapsQuery = regexp.MustCompile(`(?i)[?&]q=([+-]?\d+(?:\.\d+)?)\s*,\s*([+-]?\d+(?:\.\d+)?)`)
	reLatLon    = regexp.MustCompile(`(?i)lat[:\s]*([NS])?\s*([+-]?\d+(?:\.\d+)?)\s*[, ]\s*lon[:\s]*([EW])?\s*([+-]?\d+(?:\.\d+)?)`)
	reStamp     = regexp.MustCompile(`<(\d{2})-(\d{2})\s+(\d{2}):(\d{2})>`)
)

// ParseClassicString decodes a promoted 0x15 body:
//
//	CMD_LEN(1) | FLAG(4) | ASCII(CMD_LEN-4) | LANG(2, optional)
func ParseClassicString(body []byte) (*StringInfo, error) {
	if len(body) < 6 {
		return nil, fmt.Errorf("string body too short: %d", len(body))
	}
	cmdLen := int(body[0])
	if cmdLen < 4 || 1+cmdLen > len(body) {
		return nil, fmt.Errorf("string cmd_len=%d body=%d", cmdLen, len(body))
	}
	info := &StringInfo{
		Flag: binary.BigEndian.Uint32(body[1:5]),
		Text: sanitizeASCII(body[5 : 1+cmdLen]),
	}
	if rest := len(body) - (1 + cmdLen); rest >= 2 {
		info.Language = binary.BigEndian.Uint16(body[1+cmdLen : 1+cmdLen+2])
	}
	info.Time = parseStringStamp(info.Text)
	info.Lat, info.Lon, info.HasPos = parseStringCoords(info.Text)
	return info, nil
}

func sanitizeASCII(b []byte) string {
	var bld strings.Builder
	for _, c := range b {
		if c >= 0x20 && c < 0x7F {
			bld.WriteByte(c)
		} else if c == '\n' || c == '\r' || c == '\t' {
			bld.WriteByte(' ')
		}
	}
	s := strings.TrimSpace(bld.String())
	if !utf8.ValidString(s) {
		return strings.ToValidUTF8(s, "")
	}
	return s
}

func parseStringStamp(text string) time.Time {
	m := reStamp.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}
	}
	month, _ := strconv.Atoi(m[1])
	day, _ := strconv.Atoi(m[2])
	hour, _ := strconv.Atoi(m[3])
	min, _ := strconv.Atoi(m[4])
	now := time.Now().UTC()
	t := time.Date(now.Year(), time.Month(month), day, hour, min, 0, 0, time.UTC)
	if t.After(now.Add(180 * 24 * time.Hour)) {
		t = t.AddDate(-1, 0, 0)
	}
	return t
}

func parseStringCoords(text string) (lat, lon float64, ok bool) {
	if m := reMapsQuery.FindStringSubmatch(text); m != nil {
		lat, _ = strconv.ParseFloat(m[1], 64)
		lon, _ = strconv.ParseFloat(m[2], 64)
		return lat, lon, lat != 0 || lon != 0
	}
	if m := reLatLon.FindStringSubmatch(text); m != nil {
		lat, _ = strconv.ParseFloat(m[2], 64)
		lon, _ = strconv.ParseFloat(m[4], 64)
		if strings.EqualFold(m[1], "S") {
			lat = -lat
		}
		if strings.EqualFold(m[3], "W") {
			lon = -lon
		}
		return lat, lon, lat != 0 || lon != 0
	}
	return 0, 0, false
}

func (s *StringInfo) String() string {
	if s.HasPos {
		return fmt.Sprintf("%s lat=%.6f lon=%.6f", s.Text, s.Lat, s.Lon)
	}
	return s.Text
}

func (s *StringInfo) Telemetry() map[string]any {
	out := map[string]any{"command_reply": s.Text}
	if s.HasPos {
		out["position_type"] = "cmd"
		out["latitude"] = s.Lat
		out["longitude"] = s.Lon
		out["valid"] = true
	}
	return out
}
