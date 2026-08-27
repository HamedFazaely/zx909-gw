// Classic GT06 / Concox tracker simulator for the gt06 gateway.
//
//	go run ./cmd/toy -addr 127.0.0.1:8002 -imei 868000000000001
package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/protocol"
)

const defaultAddr = "127.0.0.1:8002"
const defaultIMEI = "868000000000001"

type toy struct {
	conn   net.Conn
	mu     sync.Mutex
	serial uint16
	lat    float64
	lon    float64
}

func main() {
	addr := flag.String("addr", defaultAddr, "gateway address")
	imei := flag.String("imei", defaultIMEI, "15-digit IMEI")
	flag.Parse()

	conn, err := net.Dial("tcp", *addr)
	if err != nil {
		log.Fatalf("dial %s: %v", *addr, err)
	}
	defer conn.Close()

	dev := &toy{
		conn:   conn,
		serial: 0,
		lat:    35.6892 + (rand.Float64()-0.5)*0.05,
		lon:    51.3890 + (rand.Float64()-0.5)*0.05,
	}
	log.Printf("connected to %s  IMEI=%s  lat=%.6f lon=%.6f", *addr, *imei, dev.lat, dev.lon)

	go dev.readLoop()

	if err := dev.send(dev.buildLogin(*imei)); err != nil {
		log.Fatal(err)
	}
	log.Println("→ login 0x01")

	time.Sleep(200 * time.Millisecond)
	dev.sendStatus()
	dev.sendGPS(5, 90)

	gpsTicker := time.NewTicker(30 * time.Second)
	statusTicker := time.NewTicker(2 * time.Minute)
	defer gpsTicker.Stop()
	defer statusTicker.Stop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-gpsTicker.C:
			const speedKmh = 5.0
			distKm := speedKmh * (30.0 / 3600.0)
			metersPerDegLon := 111320.0 * math.Cos(dev.lat*math.Pi/180)
			dev.mu.Lock()
			dev.lon += (distKm * 1000) / metersPerDegLon
			dev.mu.Unlock()
			dev.sendGPS(speedKmh, 90)
		case <-statusTicker.C:
			dev.sendStatus()
		case <-sig:
			log.Println("shutting down")
			return
		}
	}
}

func (d *toy) nextSerial() uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.serial++
	return d.serial
}

func (d *toy) send(pkt []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.conn.Write(pkt)
	return err
}

func (d *toy) sendStatus() {
	// terminal_info=0, voltage_level=6 (~100%), gsm=4, alarm=0, lang=1
	info := []byte{0x00, 0x06, 0x04, 0x00, 0x01}
	if err := d.send(buildClassic(protocol.MsgStatus, info, d.nextSerial())); err != nil {
		log.Printf("status send: %v", err)
		return
	}
	log.Println("→ status 0x13  voltage_level=6 gsm=4")
}

func (d *toy) sendGPS(speedKmh float64, heading uint16) {
	d.mu.Lock()
	lat, lon := d.lat, d.lon
	d.mu.Unlock()
	pkt := buildClassic(protocol.MsgGPSLBS, buildGPSBody(lat, lon, byte(speedKmh), heading), d.nextSerial())
	if err := d.send(pkt); err != nil {
		log.Printf("GPS send: %v", err)
		return
	}
	log.Printf("→ GPS 0x12  lat=%.6f lon=%.6f speed=%.0f heading=%d", lat, lon, speedKmh, heading)
}

func (d *toy) replyString(text string) {
	if err := d.send(buildClassic(protocol.MsgString, buildStringBody(text), d.nextSerial())); err != nil {
		log.Printf("0x15 send: %v", err)
		return
	}
	log.Printf("→ string 0x15  %s", text)
}

func (d *toy) readLoop() {
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 256)
	for {
		n, err := d.conn.Read(tmp)
		if err != nil {
			log.Printf("read: %v", err)
			return
		}
		buf = append(buf, tmp[:n]...)
		for {
			frame, consumed := takeClassic(buf)
			if consumed == 0 {
				break
			}
			buf = buf[consumed:]
			if frame == nil {
				continue
			}
			log.Printf("← proto=%s serial=%d %X", protocol.ProtoHex(frame.Proto), frame.Serial, frame.Raw)
			d.handleDownlink(frame)
		}
	}
}

func (d *toy) handleDownlink(frame *protocol.Frame) {
	if frame.Proto != protocol.MsgCommand {
		return
	}
	cmd := commandText(frame.Body)
	if cmd == "" {
		return
	}
	log.Printf("command %q", cmd)
	switch {
	case cmd == "RESET#":
		d.replyString("RESET OK")
	case cmd == "WHERE#", cmd == "URL#":
		d.mu.Lock()
		lat, lon := d.lat, d.lon
		d.mu.Unlock()
		now := time.Now().UTC()
		text := fmt.Sprintf("<%02d-%02d %02d:%02d>http://maps.google.com/maps?q=%+.6f,%+.6f",
			int(now.Month()), now.Day(), now.Hour(), now.Minute(), lat, lon)
		d.replyString(text)
	case cmd == "FINDDEVICE,ON#":
		d.replyString("FindDevice ON OK")
	case cmd == "FINDDEVICE,OFF#":
		d.replyString("FindDevice Off OK")
	case strings.HasPrefix(cmd, "TIMER,"):
		d.replyString(timerAck(cmd))
	case strings.HasPrefix(cmd, "HBT,"):
		d.replyString(hbtAck(cmd))
	case cmd == "POWEROFF#":
		log.Println("POWEROFF# ignored (matches live device)")
	default:
		d.replyString("OK")
	}
}

func commandText(body []byte) string {
	// 0x80 content: CMD_LEN | FLAG(4) | ASCII
	if len(body) < 6 {
		return ""
	}
	n := int(body[0])
	if n < 4 || 1+n > len(body) {
		n = len(body) - 1
	}
	raw := body[5 : 1+n]
	var b strings.Builder
	for _, c := range raw {
		if c >= 0x20 && c < 0x7F {
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String())
}

func timerAck(cmd string) string {
	// TIMER,T1,T2# → TIMER ACC ON:T1s,ACC OFF:T2s
	inner := strings.TrimSuffix(strings.TrimPrefix(cmd, "TIMER,"), "#")
	parts := strings.Split(inner, ",")
	if len(parts) >= 2 {
		return fmt.Sprintf("TIMER ACC ON:%ss,ACC OFF:%ss", parts[0], parts[1])
	}
	if len(parts) == 1 {
		return fmt.Sprintf("TIMER ACC ON:%ss,ACC OFF:%ss", parts[0], parts[0])
	}
	return "TIMER OK"
}

func hbtAck(cmd string) string {
	inner := strings.TrimSuffix(strings.TrimPrefix(cmd, "HBT,"), "#")
	parts := strings.Split(inner, ",")
	if len(parts) >= 2 {
		return fmt.Sprintf("HBT ACC ON:%ss,ACC OFF:%ss", parts[0], parts[1])
	}
	if len(parts) == 1 {
		return fmt.Sprintf("HBT ACC ON:%ss,ACC OFF:%ss", parts[0], parts[0])
	}
	return "HBT OK"
}

func buildClassic(proto byte, info []byte, serial uint16) []byte {
	// 78 78 | LEN | PROTO | INFO | SERIAL | CRC | 0D 0A
	// LEN = proto + info + serial + crc
	packetLen := 1 + len(info) + 2 + 2
	buf := make([]byte, 2+1+packetLen+2)
	buf[0], buf[1] = 0x78, 0x78
	buf[2] = byte(packetLen)
	buf[3] = proto
	copy(buf[4:4+len(info)], info)
	off := 4 + len(info)
	binary.BigEndian.PutUint16(buf[off:off+2], serial)
	crc := protocol.CRC16X25(buf[2 : off+2])
	binary.BigEndian.PutUint16(buf[off+2:off+4], crc)
	buf[off+4], buf[off+5] = 0x0D, 0x0A
	return buf
}

func buildLogin(imei string) func() {} // placeholder to keep gofmt happy — replaced below

func (d *toy) buildLogin(imei string) []byte {
	return buildClassic(protocol.MsgLogin, imeiToBCD(imei), d.nextSerial())
}

func buildGPSBody(lat, lon float64, speedKmh byte, heading uint16) []byte {
	now := time.Now().UTC()
	body := make([]byte, 0, 26)
	body = append(body,
		byte(now.Year()-2000),
		byte(now.Month()),
		byte(now.Day()),
		byte(now.Hour()),
		byte(now.Minute()),
		byte(now.Second()),
	)
	body = append(body, 0xC8) // GPS info: 8 sats in low nibble, typical high nibble
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(math.Abs(lat)*1_800_000))
	body = append(body, tmp[:]...)
	binary.BigEndian.PutUint32(tmp[:], uint32(math.Abs(lon)*1_800_000))
	body = append(body, tmp[:]...)
	body = append(body, speedKmh)
	cs := heading & 0x03FF
	cs |= 0x1000 // bit 12 often set on live 0x12 (North-ish / GPS valid family)
	cs |= 0x0400 // observed live flags 0x0400
	var csb [2]byte
	binary.BigEndian.PutUint16(csb[:], cs)
	body = append(body, csb[:]...)
	// Minimal LBS tail copied from live 0x12 (body_len 26).
	body = append(body, 0x01, 0xB0, 0x0B, 0xB0, 0x9F, 0x67, 0x09, 0x03)
	return body
}

func buildStringBody(text string) []byte {
	msg := []byte(text)
	cmdLen := 4 + len(msg)
	out := make([]byte, 1+cmdLen+2)
	out[0] = byte(cmdLen)
	binary.BigEndian.PutUint32(out[1:5], protocol.ClassicServerFlag)
	copy(out[5:5+len(msg)], msg)
	binary.BigEndian.PutUint16(out[5+len(msg):], 1) // language = English
	return out
}

func imeiToBCD(imei string) []byte {
	if len(imei) == 15 {
		imei = "0" + imei
	}
	if len(imei) != 16 {
		log.Fatalf("IMEI must be 15 digits, got %q", imei)
	}
	out := make([]byte, 8)
	for i := 0; i < 8; i++ {
		hi := imei[i*2] - '0'
		lo := imei[i*2+1] - '0'
		out[i] = hi<<4 | lo
	}
	return out
}

func takeClassic(buf []byte) (*protocol.Frame, int) {
	if len(buf) < 10 {
		return nil, 0
	}
	start := -1
	for i := 0; i+1 < len(buf); i++ {
		if buf[i] == 0x78 && buf[i+1] == 0x78 {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, len(buf)
	}
	if start > 0 {
		return nil, start
	}
	declared := int(buf[2])
	need := 3 + declared + 2
	if declared < 5 || need > len(buf) {
		return nil, 0
	}
	raw := append([]byte(nil), buf[:need]...)
	f := &protocol.Frame{Proto: raw[3], Raw: raw}
	infoLen := declared - 5
	if infoLen >= 0 && 4+infoLen+2 <= len(raw)-2 {
		f.Body = append([]byte(nil), raw[4:4+infoLen]...)
		f.Serial = binary.BigEndian.Uint16(raw[4+infoLen : 6+infoLen])
	}
	return f, need
}
