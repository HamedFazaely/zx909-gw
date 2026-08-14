package protocol

// CRC-16/X25 (also known as CRC-16/IBM-SDLC / CRC-16/ISO-HDLC)
// Used by GT06 / Topin / ZX909 family.
var crcTable = make([]uint16, 256)

func init() {
	const poly = 0x1021
	for i := 0; i < 256; i++ {
		crc := uint16(i) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}
		crcTable[i] = crc
	}
}

// CRC16X25 computes the CRC over data (protocol number + body + serial).
// Initial value 0xFFFF, final XOR 0xFFFF, reflected.
func CRC16X25(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc = (crc << 8) ^ crcTable[((crc>>8)^uint16(b))&0xFF]
	}
	return ^crc
}
