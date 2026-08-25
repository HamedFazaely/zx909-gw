package server

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/config"
	"github.com/HamedFazaely/zx909-gw/internal/geolocation"
	"github.com/HamedFazaely/zx909-gw/internal/mqtt"
	"github.com/HamedFazaely/zx909-gw/internal/protocol"
	"github.com/HamedFazaely/zx909-gw/internal/registry"
)

// Server accepts tracker TCP connections and publishes to ThingsBoard (or mock)
// via mqtt.Client. Optional debug REST API injects downlink commands.
type Server struct {
	cfg      config.ServerConfig
	tb       mqtt.Client
	geo      geolocation.Client
	reg      registry.Registry
	ln       net.Listener
	mu       sync.RWMutex
	sessions map[string]*Session
	wg       sync.WaitGroup
	closing  bool
}

// SafeConn serialises all writes to an underlying net.Conn.
// Reads stay unlocked — only the session goroutine reads.
type SafeConn struct {
	conn net.Conn
	mu   sync.Mutex
}

func NewSafeConn(c net.Conn) *SafeConn {
	return &SafeConn{conn: c}
}

// WriteWithDeadline is the only supported way to send bytes on the connection.
func (sc *SafeConn) WriteWithDeadline(b []byte, d time.Duration) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	_ = sc.conn.SetWriteDeadline(time.Now().Add(d))
	_, err := sc.conn.Write(b)
	return err
}

func (sc *SafeConn) Read(b []byte) (int, error) {
	return sc.conn.Read(b)
}

func (sc *SafeConn) Close() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn.Close()
}

func (sc *SafeConn) RemoteAddr() net.Addr {
	return sc.conn.RemoteAddr()
}

func (sc *SafeConn) SetReadDeadline(t time.Time) error {
	return sc.conn.SetReadDeadline(t)
}

// Session is one live tracker TCP connection keyed by IMEI after login.
type Session struct {
	IMEI        string
	Conn        *SafeConn
	Remote      string
	Connected   time.Time
	LastSeen    time.Time
	tbConnected bool // true after successful TB ConnectDevice for this session
	mu          sync.RWMutex
}

// SessionInfo is a snapshot for the debug API.
type SessionInfo struct {
	IMEI        string    `json:"imei"`
	Remote      string    `json:"remote"`
	Connected   time.Time `json:"connected"`
	LastSeen    time.Time `json:"last_seen"`
	TBConnected bool      `json:"tb_connected"`
}

func New(cfg config.ServerConfig, tb mqtt.Client, geo geolocation.Client, reg registry.Registry) *Server {
	if geo == nil {
		geo = geolocation.Disabled{}
	}
	if reg == nil {
		reg = registry.AllowAll{}
	}
	return &Server{
		cfg:      cfg,
		tb:       tb,
		geo:      geo,
		reg:      reg,
		sessions: make(map[string]*Session),
	}
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return err
	}
	s.ln = ln
	slog.Info("TCP listening", "addr", s.cfg.Listen)

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.RLock()
			closing := s.closing
			s.mu.RUnlock()
			if closing {
				return nil
			}
			slog.Error("accept error", "error", err)
			continue
		}
		s.wg.Add(1)
		go s.handleConn(NewSafeConn(conn))
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

func (s *Server) handleConn(sc *SafeConn) {
	defer s.wg.Done()
	defer sc.Close()

	remote := sc.RemoteAddr().String()
	slog.Info("tracker connected", "remote", remote)

	var (
		buf  []byte
		imei string
		sess *Session
	)

	defer func() {
		if imei != "" {
			s.removeSession(imei, sess)
			if sess != nil && sess.tbConnectedLocked() {
				_ = s.tb.DisconnectDevice(imei)
			}
			slog.Info("tracker disconnected", "imei", imei, "remote", remote)
		} else {
			slog.Info("tracker disconnected (no login)", "remote", remote)
		}
	}()

	tmp := make([]byte, 4096)
	for {
		_ = sc.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
		n, err := sc.Read(tmp)
		if err != nil {
			if err != io.EOF {
				slog.Debug("read error", "remote", remote, "error", err)
			}
			return
		}
		buf = append(buf, tmp[:n]...)
		buf = s.drainFrames(sc, remote, buf, &imei, &sess)
	}
}

func (s *Server) drainFrames(sc *SafeConn, remote string, buf []byte, imei *string, sess **Session) []byte {
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

		s.handleFrame(sc, remote, frame, imei, sess)
	}
	return buf
}

func (s *Server) writeFrame(sc *SafeConn, remote, imei string, proto byte, payload []byte, kind string) {
	if err := sc.WriteWithDeadline(payload, s.cfg.WriteTimeout); err != nil {
		slog.Warn(kind+" write failed", "remote", remote, "imei", imei, "proto", protocol.ProtoHex(proto), "error", err)
		return
	}
	slog.Info(kind,
		"remote", remote,
		"imei", imei,
		"proto", protocol.ProtoHex(proto),
		"hex", hex.EncodeToString(payload),
	)
}

// uplinkAllowed reports whether TB publish/connect is allowed for this IMEI.
// Uses the registry cache; safe to call from the session goroutine.
func (s *Server) uplinkAllowed(imei string) bool {
	if imei == "" {
		return false
	}
	if !s.reg.Enabled() {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.reg.IsRegistered(ctx, imei)
}

// publishLocation sends the unified location telemetry shape to ThingsBoard.
func (s *Server) publishLocation(imei string, ts time.Time, values map[string]any) {
	if imei == "" || !s.uplinkAllowed(imei) {
		return
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	if err := s.tb.PublishTelemetry(imei, ts, values); err != nil {
		slog.Warn("publish location failed", "imei", imei, "error", err)
	}
}

func (s *Server) publishTelemetry(imei string, ts time.Time, values map[string]any) {
	if imei == "" || !s.uplinkAllowed(imei) {
		return
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	_ = s.tb.PublishTelemetry(imei, ts, values)
}

func (s *Server) handleFrame(sc *SafeConn, remote string, frame *protocol.Frame, imei *string, sess **Session) {
	switch frame.Proto {
	case protocol.MsgLogin:
		id, err := protocol.ParseLogin(frame.Body)
		if err != nil {
			slog.Warn("login parse failed", "error", err, "remote", remote, "body", hex.EncodeToString(frame.Body))
			s.writeFrame(sc, remote, "", protocol.MsgLogin, protocol.BuildLoginACK(), "ACK")
			return
		}
		*imei = id
		*sess = s.registerSession(id, sc, remote)
		slog.Info("login ok", "imei", id, "remote", remote)
		// Must-reply first — never block the device on Paapeli/TB.
		s.writeFrame(sc, remote, id, protocol.MsgLogin, protocol.BuildLoginACK(), "ACK")
		go s.maybeConnectTB(*sess, id)

	case protocol.MsgTimeSync:
		if *sess != nil {
			(*sess).touch()
		}
		reply := protocol.BuildTimeSyncReply(time.Now())
		s.writeFrame(sc, remote, *imei, protocol.MsgTimeSync, reply, "ACK")

	case protocol.MsgParam:
		if *sess != nil {
			(*sess).touch()
		}
		reply := protocol.BuildDefaultSettings()
		s.writeFrame(sc, remote, *imei, protocol.MsgParam, reply, "ACK")

	case protocol.MsgICCID:
		if *sess != nil {
			(*sess).touch()
		}
		iccid, err := protocol.ParseICCID(frame.Body)
		if err != nil {
			slog.Debug("iccid parse failed", "imei", *imei, "error", err)
		} else {
			slog.Info("iccid", "imei", *imei, "iccid", iccid)
			s.publishTelemetry(*imei, time.Now().UTC(), map[string]any{"iccid": iccid})
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
			if st.BatteryPercent >= 0 {
				s.publishTelemetry(*imei, time.Now().UTC(), map[string]any{
					"battery": st.BatteryPercent,
				})
			}
		}
		s.writeFrame(sc, remote, *imei, protocol.MsgStatus, protocol.BuildStatusEcho(frame.Raw), "ACK")

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
				"position_type": "gps",
				"latitude":      pos.Latitude,
				"longitude":     pos.Longitude,
				"speed":         pos.SpeedKmh,
				"course":        pos.Course,
				"satellites":    pos.Satellites,
				"valid":         pos.Valid,
			}
			s.publishLocation(*imei, pos.Time, values)
			slog.Info("gps", "imei", *imei, "summary", pos.String(),
				"lat", pos.Latitude, "lon", pos.Longitude, "speed", pos.SpeedKmh,
				"course", pos.Course, "sats", pos.Satellites, "valid", pos.Valid)
		}
		dt := protocol.DatetimeFromBody(frame.Body)
		s.writeFrame(sc, remote, *imei, frame.Proto, protocol.BuildDatetimeACK(frame.Proto, dt), "ACK")

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
			if *imei != "" && s.geo.Enabled() {
				go s.resolveAndPublishLBS(*imei, wl)
			}
		}
		dt := protocol.DatetimeFromBody(frame.Body)
		s.writeFrame(sc, remote, *imei, frame.Proto, protocol.BuildDatetimeACK(frame.Proto, dt), "ACK")

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

// maybeConnectTB runs after login ACK. If the IMEI is registered, announces the
// child device to ThingsBoard. Safe to call concurrently.
func (s *Server) maybeConnectTB(sess *Session, imei string) {
	if sess == nil || imei == "" {
		return
	}
	if !s.uplinkAllowed(imei) {
		slog.Info("tracker unclaimed — protocol only, no TB uplink", "imei", imei)
		return
	}
	if err := s.tb.ConnectDevice(imei); err != nil {
		slog.Warn("TB ConnectDevice failed", "imei", imei, "error", err)
		return
	}
	sess.mu.Lock()
	sess.tbConnected = true
	sess.mu.Unlock()
	slog.Info("TB child connected", "imei", imei)
}

// resolveAndPublishLBS calls the geolocation API and publishes unified location
// telemetry only on success. Runs in its own goroutine.
func (s *Server) resolveAndPublishLBS(imei string, wl *protocol.WifiLBS) {
	if !s.uplinkAllowed(imei) {
		return
	}
	req := geolocation.Request{
		Wifi:  make([]geolocation.WifiAP, 0, len(wl.Wifi)),
		Cells: make([]geolocation.CellTower, 0, len(wl.Cells)),
	}
	for _, w := range wl.Wifi {
		req.Wifi = append(req.Wifi, geolocation.WifiAP{MAC: w.MAC, RSSI: w.RSSI})
	}
	for _, c := range wl.Cells {
		req.Cells = append(req.Cells, geolocation.CellTower{
			MCC: c.MCC, MNC: c.MNC, LAC: c.LAC, CellID: c.CellID, Signal: c.Signal,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := s.geo.Locate(ctx, req)
	if err != nil {
		slog.Debug("geolocation failed", "imei", imei, "error", err,
			"wifi", len(req.Wifi), "cells", len(req.Cells))
		return
	}

	values := map[string]any{
		"position_type": "lbs_wifi",
		"latitude":      res.Latitude,
		"longitude":     res.Longitude,
	}
	s.publishLocation(imei, wl.Time, values)
	slog.Info("lbs_wifi location", "imei", imei,
		"lat", res.Latitude, "lon", res.Longitude, "accuracy_m", res.Accuracy)
}

func (s *Server) registerSession(imei string, sc *SafeConn, remote string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.sessions[imei]; ok && old.Conn != sc {
		_ = old.Conn.Close()
	}
	sess := &Session{
		IMEI:      imei,
		Conn:      sc,
		Remote:    remote,
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

func (s *Session) tbConnectedLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tbConnected
}

func (s *Server) ListSessions() []SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SessionInfo, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sess.mu.RLock()
		out = append(out, SessionInfo{
			IMEI:        sess.IMEI,
			Remote:      sess.Remote,
			Connected:   sess.Connected,
			LastSeen:    sess.LastSeen,
			TBConnected: sess.tbConnected,
		})
		sess.mu.RUnlock()
	}
	return out
}

// SendToDevice implements command.SessionLookup.
// Returns an error if the IMEI has no live TCP session.
func (s *Server) SendToDevice(imei string, payload []byte) error {
	s.mu.RLock()
	sess, ok := s.sessions[imei]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("device %s is offline", imei)
	}
	return sess.Conn.WriteWithDeadline(payload, s.cfg.WriteTimeout)
}
