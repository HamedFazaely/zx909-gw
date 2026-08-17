// toy_device.go
// Simple ZX909 / Topin-family tracker simulator.
// go run toy_device.go [-addr host:port] [-imei 15digits]
package main

import (
	"encoding/binary"
	"flag"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultAddr = "127.0.0.1:8002"
	defaultIMEI = "861971080061526"
)

func main() {
	addr := flag.String("addr", defaultAddr, "gateway address")
	imei := flag.String("imei", defaultIMEI, "15-digit IMEI")
	flag.Parse()

	conn, err := net.Dial("tcp", *addr)
	if err != nil {
		log.Fatalf("dial %s: %v", *addr, err)
	}
	defer conn.Close()
	log.Printf("connected to %s  IMEI=%s", *addr, *imei)

	// Drain server replies in the background
	go readLoop(conn)

	// 1. Login
	if err := send(conn, buildLogin(*imei)); err != nil {
		log.Fatal(err)
	}
	log.Println("→ login 0x01")

	// 2. Time-sync request (real devices do this right after login)
	time.Sleep(200 * time.Millisecond)
	if err := send(conn, buildTimeSyncReq()); err != nil {
		log.Fatal(err)
	}
	log.Println("→ time-sync request 0x30")

	// Starting position (Tehran-ish) + small random jitter
	lat := 35.6892 + (rand.Float64()-0.5)*0.05
	lon := 51.3890 + (rand.Float64()-0.5)*0.05
	log.Printf("start position  lat=%.6f  lon=%.6f", lat, lon)

	gpsTicker := time.NewTicker(30 * time.Second)
	statusTicker := time.NewTicker(2 * time.Minute)
	defer gpsTicker.Stop()
	defer statusTicker.Stop()

	// First GPS immediately so we see something right away
	sendGPS(conn, lat, lon)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-gpsTicker.C:
			// 5 km/h due east
			const speedKmh = 5.0
			dtH := 30.0 / 3600.0 // hours
			distKm := speedKmh * dtH
			// metres east → degrees longitude
			metersPerDegLon := 111320.0 * math.Cos(lat*math.Pi/180)
			lon += (distKm * 1000) / metersPerDegLon
			sendGPS(conn, lat, lon)

		case <-statusTicker.C:
			if err := send(conn, buildStatus()); err != nil {
				log.Printf("status send: %v", err)
				return
			}
			log.Println("→ status 0x13  battery=100%")

		case <-sig:
			log.Println("shutting down")
			return
		}
	}
}

func sendGPS(conn net.Conn, lat, lon float64) {
	pkt := buildGPS(lat, lon, 5 /*km/h*/, 90 /*heading*/)
	if err := send(conn, pkt); err != nil {
		log.Printf("GPS send: %v", err)
		return
	}
	log.Printf("→ GPS 0x10  lat=%.6f  lon=%.6f  speed=5 km/h  heading=90", lat, lon)
}

// ---------- packet builders ----------

func buildLogin(imei string) []byte {
	// 78 78 | 0A | 01 | IMEI(8 BCD) | softVer(1) | 0D 0A
	b := make([]byte, 0, 16)
	b = append(b, 0x78, 0x78, 0x0A, 0x01)
	b = append(b, imeiToBCD(imei)...)
	b = append(b, 0x39) // pretend firmware 1.3.9
	b = append(b, 0x0D, 0x0A)
	return b
}

func buildTimeSyncReq() []byte {
	// 78 78 01 30 0D 0A
	return []byte{0x78, 0x78, 0x01, 0x30, 0x0D, 0x0A}
}

func buildGPS(lat, lon float64, speedKmh, heading uint16) []byte {
	// Basic (pre-altitude) 0x10 layout from 365GPS doc:
	// 78 78 | 12 | 10 | DT(6) | 9C | lat(4) | lon(4) | speed(1) | courseStatus(2) | 0D 0A
	now := time.Now().UTC()
	dt := []byte{
		byte(now.Year() - 2000),
		byte(now.Month()),
		byte(now.Day()),
		byte(now.Hour()),
		byte(now.Minute()),
		byte(now.Second()),
	}

	latRaw := uint32(math.Abs(lat) * 1_800_000)
	lonRaw := uint32(math.Abs(lon) * 1_800_000)

	// courseStatus: keep the same flag nibble that real devices use for
	// North + East + GPS-fixed, then 10-bit heading.
	// Example from doc (heading 96) was 0x3460 → flags 0x34, heading in low 10 bits.
	statusByte := byte(0x34) // N, E, fixed (matches observed traffic)
	statusByte = (statusByte & 0xFC) | byte((heading>>8)&0x03)
	courseByte := byte(heading & 0xFF)

	buf := make([]byte, 0, 24)
	buf = append(buf, 0x78, 0x78, 0x12, 0x10)
	buf = append(buf, dt...)
	buf = append(buf, 0x9C) // GPS-info length nibble + 12 sats
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], latRaw)
	buf = append(buf, tmp[:]...)
	binary.BigEndian.PutUint32(tmp[:], lonRaw)
	buf = append(buf, tmp[:]...)
	buf = append(buf, byte(speedKmh))
	buf = append(buf, statusByte, courseByte)
	buf = append(buf, 0x0D, 0x0A)
	return buf
}

func buildStatus() []byte {
	// 78 78 06 13  battery  softVer  tz  intervalMin  0D 0A
	// battery=100 (0x64), soft=0x39, tz=+3 (Iran), interval=2 min
	return []byte{0x78, 0x78, 0x06, 0x13, 0x64, 0x39, 0x03, 0x02, 0x0D, 0x0A}
}

func imeiToBCD(imei string) []byte {
	if len(imei) == 15 {
		imei = "0" + imei // 16 nibbles
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

func send(conn net.Conn, pkt []byte) error {
	_, err := conn.Write(pkt)
	return err
}

func readLoop(conn net.Conn) {
	buf := make([]byte, 512)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Printf("read: %v", err)
			return
		}
		log.Printf("← %X", buf[:n])
	}
}
