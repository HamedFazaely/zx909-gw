package protocol

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// WifiAP is one Wi-Fi access point from a 0x1A / 0x1B packet.
type WifiAP struct {
	MAC  string // "aa:bb:cc:dd:ee:ff"
	RSSI int    // raw signal byte (higher is usually stronger on this firmware)
}

// CellTower is one GSM/LTE cell from a 0x1A / 0x1B packet.
type CellTower struct {
	MCC    int
	MNC    int
	LAC    int
	CellID int64
	Signal int
}

// WifiLBS is the decoded content of protocol 0x1A or 0x1B.
type WifiLBS struct {
	Time  time.Time
	Wifi  []WifiAP
	Cells []CellTower
	Raw   []byte
}

// ParseWifiLBS decodes Topin / ZX909 0x1A and 0x1B bodies.
//
// Observed layout (from live ZX909_EU captures):
//
//	datetime     6 bytes BCD (YY MM DD HH MM SS)
//	wifi section  optional, N × 7 bytes (MAC 6 + RSSI 1), no count byte
//	lbs section   count(1) + MCC(2) + MNC(1) + count × (LAC 2 + CellID 4 + Signal 1)
//
// Wi-Fi ends when we see a plausible LBS header: small count (1–6) followed by
// MCC (we accept any non-zero MCC; Iran is 0x01B0 = 432).
func ParseWifiLBS(body []byte) (*WifiLBS, error) {
	if len(body) < 6 {
		return nil, fmt.Errorf("wifi/lbs body too short: %d", len(body))
	}

	out := &WifiLBS{Raw: append([]byte(nil), body...)}

	// DateTime is BCD on 0x1A/0x1B (unlike binary on 0x11 GPS).
	yy, mm, dd := bcdByte(body[0]), bcdByte(body[1]), bcdByte(body[2])
	hh, mi, ss := bcdByte(body[3]), bcdByte(body[4]), bcdByte(body[5])
	if yy < 0 || mm < 1 || mm > 12 || dd < 1 || dd > 31 || hh > 23 || mi > 59 || ss > 59 {
		// Fall back to binary interpretation if BCD looks invalid
		yy, mm, dd = int(body[0]), int(body[1]), int(body[2])
		hh, mi, ss = int(body[3]), int(body[4]), int(body[5])
	}
	out.Time = time.Date(2000+yy, time.Month(mm), dd, hh, mi, ss, 0, time.UTC)

	rest := body[6:]
	lbsStart := findLBSHeader(rest)

	wifiBytes := rest
	lbsBytes := []byte(nil)
	if lbsStart >= 0 {
		wifiBytes = rest[:lbsStart]
		lbsBytes = rest[lbsStart:]
	}

	// Wi-Fi: groups of 7 bytes
	for i := 0; i+7 <= len(wifiBytes); i += 7 {
		mac := wifiBytes[i : i+6]
		rssi := int(wifiBytes[i+6])
		out.Wifi = append(out.Wifi, WifiAP{
			MAC:  formatMAC(mac),
			RSSI: rssi,
		})
	}

	if len(lbsBytes) >= 4 {
		count := int(lbsBytes[0])
		mcc := int(binary.BigEndian.Uint16(lbsBytes[1:3]))
		mnc := int(lbsBytes[3])
		pos := 4
		// Prefer 7-byte cells (LAC2 + CI4 + Signal1); fall back to 6-byte (LAC2 + CI3 + Signal1)
		cellSize := 7
		if count > 0 && pos+count*7 > len(lbsBytes) && pos+count*6 <= len(lbsBytes) {
			cellSize = 6
		}
		for i := 0; i < count && pos+cellSize <= len(lbsBytes); i++ {
			lac := int(binary.BigEndian.Uint16(lbsBytes[pos : pos+2]))
			var ci int64
			var sig int
			if cellSize == 7 {
				ci = int64(binary.BigEndian.Uint32(lbsBytes[pos+2 : pos+6]))
				sig = int(lbsBytes[pos+6])
			} else {
				ci = int64(uint32(lbsBytes[pos+2])<<16 | uint32(lbsBytes[pos+3])<<8 | uint32(lbsBytes[pos+4]))
				sig = int(lbsBytes[pos+5])
			}
			out.Cells = append(out.Cells, CellTower{
				MCC:    mcc,
				MNC:    mnc,
				LAC:    lac,
				CellID: ci,
				Signal: sig,
			})
			pos += cellSize
		}
	}

	return out, nil
}

// findLBSHeader returns the index of count+MCC+MNC in rest, or -1.
func findLBSHeader(rest []byte) int {
	for i := 0; i+4 <= len(rest); i++ {
		count := int(rest[i])
		if count < 1 || count > 6 {
			continue
		}
		mcc := binary.BigEndian.Uint16(rest[i+1 : i+3])
		// Plausible MCC range (001–999); Iran = 432
		if mcc == 0 || mcc > 999 {
			continue
		}
		// Prefer alignments where remaining length fits count cells (6 or 7 bytes each)
		remain := len(rest) - (i + 4)
		if remain >= count*6 {
			return i
		}
	}
	return -1
}

func bcdByte(b byte) int {
	hi, lo := int(b>>4), int(b&0x0F)
	if hi > 9 || lo > 9 {
		return -1
	}
	return hi*10 + lo
}

func formatMAC(mac []byte) string {
	parts := make([]string, len(mac))
	for i, b := range mac {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, ":")
}

// String returns a human-readable summary for logs.
func (w *WifiLBS) String() string {
	var b strings.Builder
	b.WriteString("time=")
	b.WriteString(w.Time.Format("2006-01-02 15:04:05 UTC"))
	if len(w.Wifi) > 0 {
		b.WriteString(fmt.Sprintf(" wifi=%d[", len(w.Wifi)))
		for i, ap := range w.Wifi {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(fmt.Sprintf("%s(%d)", ap.MAC, ap.RSSI))
		}
		b.WriteByte(']')
	}
	if len(w.Cells) > 0 {
		b.WriteString(fmt.Sprintf(" cells=%d[", len(w.Cells)))
		for i, c := range w.Cells {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(fmt.Sprintf("%d/%d/LAC=%d/CI=%d/sig=%d", c.MCC, c.MNC, c.LAC, c.CellID, c.Signal))
		}
		b.WriteByte(']')
	}
	if len(w.Wifi) == 0 && len(w.Cells) == 0 && len(w.Raw) > 0 {
		b.WriteString(" raw=")
		b.WriteString(hex.EncodeToString(w.Raw))
	}
	return b.String()
}
