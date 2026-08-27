package protocol

import "encoding/binary"

// promoteClassic upgrades a trailer-extracted frame when it is a well-formed
// classic GT06 / Concox packet: the length byte matches the frame size and
// CRC-ITU over (length … serial) verifies.
//
// On success the information content is stripped of serial+CRC, Serial is
// populated, and Classic is set so the server can emit PDF-style ACKs
// (78 78 05 PROTO SERIAL CRC 0D 0A) instead of the 365GPS short replies.
//
// Topin / 365GPS packets fail the length or CRC check and are left untouched.
func promoteClassic(f *Frame) {
	if f == nil {
		return
	}
	raw := f.Raw
	if len(raw) < 10 || raw[0] != 0x78 || raw[1] != 0x78 {
		return
	}
	declared := int(raw[2])
	if declared < 5 {
		return
	}
	// 78 78 | len | (len bytes) | 0D 0A
	if 3+declared+2 != len(raw) {
		return
	}
	if raw[len(raw)-2] != 0x0D || raw[len(raw)-1] != 0x0A {
		return
	}
	crcData := raw[2 : 1+declared] // Packet Length through Serial
	got := binary.BigEndian.Uint16(raw[1+declared : 3+declared])
	if CRC16X25(crcData) != got {
		return
	}
	infoLen := declared - 5 // proto + info + serial + crc = declared
	if infoLen < 0 {
		return
	}
	f.Body = append([]byte(nil), raw[4:4+infoLen]...)
	f.Serial = binary.BigEndian.Uint16(raw[4+infoLen : 6+infoLen])
	f.Classic = true
}
