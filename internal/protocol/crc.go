package protocol

// CRC-ITU (CRC-16/X25 / IBM-SDLC / ISO-HDLC) as used by GT06 / Topin / ZX909.
//
// Algorithm (matches classic Concox/GT06 Appendix A behaviour):
//   - init 0xFFFF
//   - reflected (right-shift) with poly 0x8408
//   - final invert (~crc)
//
// Scope per protocol doc: from Packet Length through Information Serial Number
// (inclusive). Header (78 78 / 79 79) and trailer (0D 0A) are excluded.
func CRC16X25(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}
