package protocol

import (
	"encoding/binary"
	"fmt"
)

const (
	MsgCommand byte = 0x80
	MsgString  byte = 0x15

	ClassicServerFlag uint32 = 0x00000001
)

func BuildClassicCommand(command string, serial uint16) []byte {
	cmd := []byte(command)
	cmdLen := 4 + len(cmd)
	packetLen := 1 + 1 + cmdLen + 2 + 2

	buf := make([]byte, 2+1+packetLen+2)
	buf[0], buf[1] = 0x78, 0x78
	buf[2] = byte(packetLen)
	buf[3] = MsgCommand
	buf[4] = byte(cmdLen)
	binary.BigEndian.PutUint32(buf[5:9], ClassicServerFlag)
	copy(buf[9:9+len(cmd)], cmd)
	off := 9 + len(cmd)
	binary.BigEndian.PutUint16(buf[off:off+2], serial)
	crc := CRC16X25(buf[2 : off+2])
	binary.BigEndian.PutUint16(buf[off+2:off+4], crc)
	buf[off+4], buf[off+5] = 0x0D, 0x0A
	return buf
}

func BuildClassicRestart(serial uint16) []byte {
	return BuildClassicCommand("RESET#", serial)
}

func BuildClassicShutdown(serial uint16) []byte {
	return BuildClassicCommand("POWEROFF#", serial)
}

func BuildClassicLocate(serial uint16) []byte {
	return BuildClassicCommand("WHERE#", serial)
}

func BuildClassicFind(on bool, serial uint16) []byte {
	if on {
		return BuildClassicCommand("FINDDEVICE,ON#", serial)
	}
	return BuildClassicCommand("FINDDEVICE,OFF#", serial)
}

func clampTimerSec(sec int) int {
	if sec < 5 {
		return 5
	}
	if sec > 18000 {
		return 18000
	}
	return sec
}

// BuildClassicTimer is TIMER,T1,T2# — ACC ON / ACC OFF seconds.
// Confirmed on 868022030668730: TIMER,60,180# → "TIMER ACC ON:60s,ACC OFF:180s".
func BuildClassicTimer(accOnSec, accOffSec int, serial uint16) []byte {
	accOnSec = clampTimerSec(accOnSec)
	accOffSec = clampTimerSec(accOffSec)
	return BuildClassicCommand(fmt.Sprintf("TIMER,%d,%d#", accOnSec, accOffSec), serial)
}

func BuildClassicUploadInterval(seconds uint16, serial uint16) []byte {
	return BuildClassicTimer(int(seconds), int(seconds), serial)
}

func BuildClassicStatusInterval(minutes int, serial uint16) []byte {
	if minutes < 1 {
		minutes = 1
	}
	return BuildClassicCommand(fmt.Sprintf("HBT,%d#", minutes*60), serial)
}
