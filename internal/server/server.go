package server

import (
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/config"
	"github.com/HamedFazaely/zx909-gw/internal/mqtt"
	"github.com/HamedFazaely/zx909-gw/internal/protocol"
)

// Server accepts tracker TCP connections and forwards data to ThingsBoard.
type Server struct {
	cfg      config.ServerConfig
	tb       *mqtt.GatewayClient
	ln       net.Listener
	mu       sync.Mutex
	sessions map[string]*Session // imei -> session
	wg       sync.WaitGroup
	closing  bool
}

type Session struct {
	IMEI       string
	Conn       net.Conn
	Connected  time.Time
	LastSeen   time.Time
	mu         sync.Mutex
}

func New(cfg config.ServerConfig, tb *mqtt.GatewayClient) *Server {
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
		}
	}()

	tmp := make([]byte, 2048)
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

		for {
			frame, consumed, err := protocol.ExtractFrame(buf)
			if err == protocol.ErrIncomplete {
				break
			}
			if err != nil {
				// Resync: drop one byte and try again
				slog.Debug("frame error, resync", "error", err, "remote", remote)
				buf = buf[1:]
				continue
			}
			buf = buf[consumed:]

			switch frame.Proto {
			case protocol.MsgLogin:
				id, err := protocol.ParseLogin(frame.Body)
				if err != nil {
					slog.Warn("login parse failed", "error", err, "remote", remote)
					continue
				}
				imei = id
				sess = s.registerSession(imei, conn)
				_ = s.tb.ConnectDevice(imei)
				ack := protocol.BuildACK(protocol.MsgLogin, frame.Serial)
				_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
				_, _ = conn.Write(ack)
				slog.Info("login ok", "imei", imei, "remote", remote)

			case protocol.MsgStatus:
				// Heartbeat / status – just ACK
				if sess != nil {
					sess.touch()
				}
				ack := protocol.BuildACK(protocol.MsgStatus, frame.Serial)
				_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
				_, _ = conn.Write(ack)

			case protocol.MsgGPS, protocol.MsgGPSLBS, protocol.MsgGPS2, protocol.MsgGPSOffline:
				if imei == "" {
					slog.Debug("GPS before login, ignoring", "remote", remote)
					continue
				}
				if sess != nil {
					sess.touch()
				}
				pos, err := protocol.ParseGPS(frame.Body)
				if err != nil {
					slog.Debug("GPS parse failed", "imei", imei, "error", err, "body_len", len(frame.Body))
					// Still ACK so the device does not spam
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
					if err := s.tb.PublishTelemetry(imei, ts, values); err != nil {
						slog.Warn("publish telemetry failed", "imei", imei, "error", err)
					} else {
						slog.Info("telemetry", "imei", imei,
							"lat", pos.Latitude, "lon", pos.Longitude, "speed", pos.SpeedKmh)
					}
				}
				// Many firmwares expect an ACK for GPS packets as well
				ack := protocol.BuildACK(frame.Proto, frame.Serial)
				_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
				_, _ = conn.Write(ack)

			default:
				slog.Debug("unhandled proto", "proto", frame.Proto, "imei", imei, "remote", remote)
				// Best-effort ACK for unknown types that look like they need one
				ack := protocol.BuildACK(frame.Proto, frame.Serial)
				_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
				_, _ = conn.Write(ack)
			}
		}
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
