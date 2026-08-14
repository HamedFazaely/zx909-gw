package protocol

import (
	"encoding/hex"
	"fmt"
)

// ParseLogin extracts IMEI from a login (0x01) body.
// Body layout for classic GT06/Topin: 8 bytes BCD IMEI.
func ParseLogin(body []byte) (imei string, err error) {
	if len(body) < 8 {
		return "", fmt.Errorf("login body too short: %d", len(body))
	}
	// First 8 bytes are BCD-encoded IMEI (15 digits, last nibble often 0 or filler)
	bcd := body[:8]
	raw := make([]byte, 0, 16)
	for _, b := range bcd {
		high := (b >> 4) & 0x0F
		low := b & 0x0F
		raw = append(raw, '0'+high, '0'+low)
	}
	s := string(raw)
	// Trim trailing filler if present (common is trailing 0 or F)
	if len(s) == 16 && (s[15] == '0' || s[15] == 'F' || s[15] == 'f') {
		s = s[:15]
	}
	if len(s) < 15 {
		return "", fmt.Errorf("unexpected IMEI length from BCD: %q (hex %s)", s, hex.EncodeToString(bcd))
	}
	return s[:15], nil
}
