package protocol

import (
	"encoding/hex"
	"fmt"
)

// ParseLogin extracts IMEI from a login (0x01) body.
//
// Observed ZX909_EU login content (after proto byte):
//   08 61 97 10 80 06 15 26 09
// First 8 bytes are BCD IMEI. Leading nibble is often 0 filler → 861971080061526.
func ParseLogin(body []byte) (imei string, err error) {
	if len(body) < 8 {
		return "", fmt.Errorf("login body too short: %d (hex %s)", len(body), hex.EncodeToString(body))
	}

	bcd := body[:8]
	raw := make([]byte, 0, 16)
	for _, b := range bcd {
		high := (b >> 4) & 0x0F
		low := b & 0x0F
		// BCD digits should be 0-9; treat A-F as 0 for robustness
		if high > 9 {
			high = 0
		}
		if low > 9 {
			low = 0
		}
		raw = append(raw, '0'+high, '0'+low)
	}
	s := string(raw)

	// Common Topin pattern: 16 hex digits with leading 0 → drop it to get 15-digit IMEI
	if len(s) == 16 && s[0] == '0' {
		s = s[1:]
	}
	// Also trim a trailing filler 0/F if still 16 chars
	if len(s) == 16 && (s[15] == '0' || s[15] == 'F' || s[15] == 'f') {
		s = s[:15]
	}
	if len(s) < 15 {
		return "", fmt.Errorf("unexpected IMEI from BCD: %q (hex %s)", s, hex.EncodeToString(bcd))
	}
	return s[:15], nil
}
