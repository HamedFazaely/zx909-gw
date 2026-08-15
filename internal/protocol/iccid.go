package protocol

import (
	"fmt"
	"unicode"
)

// ParseICCID extracts the ASCII ICCID from a 0xB3 body.
func ParseICCID(body []byte) (string, error) {
	if len(body) == 0 {
		return "", fmt.Errorf("empty ICCID body")
	}
	// Trim non-printable / non-digit tail if any
	out := make([]byte, 0, len(body))
	for _, b := range body {
		if b == 0 {
			break
		}
		if unicode.IsPrint(rune(b)) {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return "", fmt.Errorf("no printable ICCID in body")
	}
	return string(out), nil
}
