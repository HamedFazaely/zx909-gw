package server

import (
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/config"
	"github.com/HamedFazaely/zx909-gw/internal/mqtt"
	"github.com/HamedFazaely/zx909-gw/internal/protocol"
)

// Server accepts tracker TCP connections and forwards data to ThingsBoard
// (or a mock) via the mqtt.Client interface.
//
// When VendorForward is enabled it also proxies to the official vendor so we
// can compare our ACKs with theirs while validating the protocol.
type Server struct {
	cfg      config.ServerConfig
	tb       mqtt.Client
	ln       net.Listener
	mu       sync.Mutex
	sessions map[string]*Session
	wg       sync.WaitGroup
	closing  bool
}

type Session struct {
	IMEI      string
	Conn      net.Conn
	Connected time.Time
	LastSeen  time.Time
	mu        sync.Mutex
}

func New(cfg config.ServerConfig, tb mqtt.Client) *Server {
	return &Server{
		cfg:      cfg,
		tb:       tb,
		sessions: make(map[string]*Session),
	}
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return err
	}
	s.ln = ln

	if s.cfg.VendorForward.Enabled {
		slog.Warn("vendor_forward ENABLED — traffic is proxied to official server (debug only)",
			"host", s.cfg.VendorForward.Host,
			"port", s.cfg.VendorForward.Port,
		)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			if closing {
				return nil
			}
			slog.Error("accept error", "error", err)
			continue
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) Shutdown() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	if s.ln != nil {
		_ = s.ln.Close()
	}
	s.wg.Wait()
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	remote := conn.RemoteAddr().String()
	slog.Info("tracker connected", "remote", remote)

	if s.cfg.VendorForward.Enabled {
		s.handleConnWithVendor(conn, remote)
		return
	}
	s.handleConnStandalone(conn, remote)
}

func (s *Server) handleConnStandalone(conn net.Conn, remote string) {
	var (
		buf  []byte
		imei string
		sess *Session
	)

	defer func() {
		if imei != "" {
			s.removeSession(imei, sess)
			_ = s.tb.DisconnectDevice(imei)
			slog.Info("tracker disconnected", "imei", imei, "remote", remote)
		} else {
			slog.Info("tracker disconnected (no login)", "remote", remote)
		}
	}()

	tmp := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
		n, err := conn.Read(tmp)
		if err != nil {
			if err != io.EOF {
				slog.Debug("read error", "remote", remote, "error", err)
			}
			return
		}
		buf = append(buf, tmp[:n]...)
		buf = s.drainFrames(conn, remote, buf, &imei, &sess, true)
	}
}

func (s *Server) handleConnWithVendor(deviceConn net.Conn, remote string) {
	vendorAddr := net.JoinHostPort(s.cfg.VendorForward.Host, fmt.Sprintf("%d", s.cfg.VendorForward.Port))
	vendorConn, err := net.DialTimeout("tcp", vendorAddr, 10*time.Second)
	if err != nil {
		slog.Error("vendor dial failed, falling back to standalone",
			"remote", remote, "vendor", vendorAddr, "error", err)
		s.handleConnStandalone(deviceConn, remote)
		return
	}
	defer vendorConn.Close()
	slog.Info("vendor connected", "remote", remote, "vendor", vendorAddr)

	var (
		imei string
		sess *Session
		mu   sync.Mutex
	)

	defer func() {
		mu.Lock()
		id := imei
		se := sess
		mu.Unlock()
		if id != "" {
			s.removeSession(id, se)
			_ = s.tb.DisconnectDevice(id)
			slog.Info("tracker disconnected", "imei", id, "remote", remote)
		} else {
			slog.Info("tracker disconnected (no login)", "remote", remote)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer vendorConn.Close()
		defer deviceConn.Close()

		var buf []byte
		tmp := make([]byte, 4096)
		for {
			_ = deviceConn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
			n, err := deviceConn.Read(tmp)
			if err != nil {
				if err != io.EOF {
					slog.Debug("device read error", "remote", remote, "error", err)
				}
				return
			}

			chunk := tmp[:n]
			_ = vendorConn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
			if _, err := vendorConn.Write(chunk); err != nil {
				slog.Debug("vendor write error", "remote", remote, "error", err)
				return
			}

			buf = append(buf, chunk...)
			mu.Lock()
			buf = s.drainFrames(deviceConn, remote, buf, &imei, &sess, false)
			mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		defer vendorConn.Close()
		defer deviceConn.Close()

		tmp := make([]byte, 4096)
		for {
			_ = vendorConn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
			n, err := vendorConn.Read(tmp)
			if err != nil {
				if err != io.EOF {
					slog.Debug("vendor read error", "remote", remote, "error", err)
				}
				return
			}

			chunk := tmp[:n]
			slog.Info("S2C", "remote", remote, "n", n, "hex", hex.EncodeToString(chunk))

			_ = deviceConn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
			if _, err := deviceConn.Write(chunk); err != nil {
				slog.Debug("device write error", "remote", remote, "error", err)
				return
			}
		}
	}()

	wg.Wait()
}

func (s *Server) drainFrames(conn net.Conn, remote string, buf []byte, imei *string, sess **Session, sendACKs bool) []byte {
	for {
		frame, consumed, err := protocol.ExtractFrame(buf)
		if err == protocol.ErrIncomplete {
			break
		}
		if err != nil {
			slog.Warn("frame error, resync",
				"error", err,
				"remote", remote,
				"buf_hex", hex.EncodeToString(buf),
				"consumed", consumed,
			)
			if consumed > 0 && consumed <= len(buf) {
				buf = buf[consumed:]
			} else if len(buf) > 0 {
				buf = buf[1:]
			}
			continue
		}
		buf = buf[consumed:]

		slog.Info("frame",
			"remote", remote,
			"imei", *imei,
			"proto", protocol.ProtoHex(frame.Proto),
			"body_len", len(frame.Body),
			"raw", frame.Hex(),
		)

		s.handleFrame(conn, remote, frame, imei, sess, sendACKs)
	}
	return buf
}

func (s *Server) writeACK(conn net.Conn, remote, imei string, proto byte, ack []byte) {
	_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	_, err := conn.Write(ack)
	if err != nil {
		slog.Warn("ACK write failed", "remote", remote, "imei", imei, "proto", protocol.ProtoHex(proto), "error", err)
		return
	}
	slog.Info("ACK",
		"remote", remote,
		"imei", imei,
		"proto", protocol.ProtoHex(proto),
		"hex", hex.EncodeToString(ack),
	)
}

func (s *Server) handleFrame(conn net.Conn, remote string, frame *protocol.Frame, imei *string, sess **Session, sendACKs bool) {
	switch frame.Proto {
	case protocol.MsgLogin:
		id, err := protocol.ParseLogin(frame.Body)
		if err != nil {
			slog.Warn("login parse failed", "error", err, "remote", remote, "body", hex.EncodeToString(frame.Body))
			if sendACKs {
				s.writeACK(conn, remote, "", protocol.MsgLogin, protocol.BuildLoginACK())
			}
			return
		}
		*imei = id
		*sess = s.registerSession(id, conn)
		_ = s.tb.ConnectDevice(id)
		slog.Info("login ok", "imei", id, "remote", remote)
		if sendACKs {
			s.writeACK(conn, remote, id, protocol.MsgLogin, protocol.BuildLoginACK())
		}

	case protocol.MsgStatus:
		if *sess != nil {
			(*sess).touch()
		}
		st, err := protocol.ParseStatus(frame.Body)
		if err != nil {
			slog.Debug("status parse failed", "imei", *imei, "error", err, "body", hex.EncodeToString(frame.Body))
		} else {
			slog.Info("status", "imei", *imei, "summary", st.String(),
				"battery", st.BatteryPercent, "sw", st.SoftwareVer, "tz", st.Timezone)
			if *imei != "" && st.BatteryPercent >= 0 {
				_ = s.tb.PublishTelemetry(*imei, time.Now().UTC(), map[string]any{
					"battery": st.BatteryPercent,
				})
			}
		}
		if sendACKs {
			s.writeACK(conn, remote, *imei, protocol.MsgStatus, protocol.BuildStatusEcho(frame.Raw))
		}

	case protocol.MsgGPS, protocol.MsgGPSLBS, protocol.MsgGPS2, protocol.MsgGPSOffline:
		if *imei == "" {
			slog.Debug("GPS before login, ignoring", "remote", remote)
			return
		}
		if *sess != nil {
			(*sess).touch()
		}
		pos, err := protocol.ParseGPS(frame.Body)
		if err != nil {
			slog.Debug("GPS parse failed", "imei", *imei, "error", err,
				"body_len", len(frame.Body), "proto", protocol.ProtoHex(frame.Proto), "body", hex.EncodeToString(frame.Body))
		} else {
			values := map[string]any{
				"latitude":   pos.Latitude,
				"longitude":  pos.Longitude,
				"speed":      pos.SpeedKmh,
				"course":     pos.Course,
				"satellites": pos.Satellites,
				"valid":      pos.Valid,
			}
			ts := pos.Time
			if ts.IsZero() {
				ts = time.Now().UTC()
			}
			if err := s.tb.PublishTelemetry(*imei, ts, values); err != nil {
				slog.Warn("publish telemetry failed", "imei", *imei, "error", err)
			} else {
				slog.Info("gps", "imei", *imei, "summary", pos.String(),
					"lat", pos.Latitude, "lon", pos.Longitude, "speed", pos.SpeedKmh,
					"course", pos.Course, "sats", pos.Satellites, "valid", pos.Valid)
			}
		}
		if sendACKs {
			dt := protocol.DatetimeFromBody(frame.Body)
			s.writeACK(conn, remote, *imei, frame.Proto, protocol.BuildDatetimeACK(frame.Proto, dt))
		}

	case protocol.MsgWifiLBS, protocol.MsgWifiLBS2, protocol.MsgOfflineWifi, protocol.MsgOnlineWifi:
		if *sess != nil {
			(*sess).touch()
		}
		wl, err := protocol.ParseWifiLBS(frame.Body)
		if err != nil {
			slog.Debug("wifi/lbs parse failed", "imei", *imei, "error", err,
				"proto", protocol.ProtoHex(frame.Proto), "body", hex.EncodeToString(frame.Body))
		} else {
			slog.Info("wifi/lbs", "imei", *imei, "proto", protocol.ProtoHex(frame.Proto), "summary", wl.String(),
				"wifi_count", len(wl.Wifi), "cell_count", len(wl.Cells))
			if *imei != "" {
				values := map[string]any{
					"lbs_wifi_count": len(wl.Wifi),
					"lbs_cell_count": len(wl.Cells),
					"position_type":  "lbs_wifi",
				}
				if len(wl.Cells) > 0 {
					c := wl.Cells[0]
					values["mcc"] = c.MCC
					values["mnc"] = c.MNC
					values["lac"] = c.LAC
					values["cell_id"] = c.CellID
				}
				ts := wl.Time
				if ts.IsZero() {
					ts = time.Now().UTC()
				}
				_ = s.tb.PublishTelemetry(*imei, ts, values)
			}
		}
		if sendACKs {
			dt := protocol.DatetimeFromBody(frame.Body)
			s.writeACK(conn, remote, *imei, frame.Proto, protocol.BuildDatetimeACK(frame.Proto, dt))
		}

	default:
		if *sess != nil {
			(*sess).touch()
		}
		slog.Info("unhandled",
			"proto", protocol.ProtoHex(frame.Proto),
			"imei", *imei,
			"remote", remote,
			"body_len", len(frame.Body),
			"body", hex.EncodeToString(frame.Body),
			"raw", frame.Hex(),
		)
	}
}

func (s *Server) registerSession(imei string, conn net.Conn) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.sessions[imei]; ok && old.Conn != conn {
		_ = old.Conn.Close()
	}
	sess := &Session{
		IMEI:      imei,
		Conn:      conn,
		Connected: time.Now(),
		LastSeen:  time.Now(),
	}
	s.sessions[imei] = sess
	return sess
}

func (s *Server) removeSession(imei string, sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.sessions[imei]; ok && cur == sess {
		delete(s.sessions, imei)
	}
}

func (s *Session) touch() {
	s.mu.Lock()
	s.LastSeen = time.Now()
	s.mu.Unlock()
}
