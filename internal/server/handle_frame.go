package server

import (
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/protocol"
)

func (s *Server) handleFrame(sc *SafeConn, remote string, frame *protocol.Frame, imei *string, sess **Session) {
	switch frame.Proto {
	case protocol.MsgLogin:
		id, err := protocol.ParseLogin(frame.Body)
		if err != nil {
			slog.Warn("login parse failed", "error", err, "remote", remote, "body", hex.EncodeToString(frame.Body))
			s.replyLogin(sc, remote, "", frame)
			return
		}
		*imei = id
		*sess = s.registerSession(id, sc, remote)
		slog.Info("login ok", "imei", id, "remote", remote, "serial", frame.Serial)
		s.replyLogin(sc, remote, id, frame)
		go s.maybeConnectTB(*sess, id)
	case protocol.MsgTimeSync, protocol.MsgParam:
		// Topin handshake packets — ignore on this product.
		if *sess != nil {
			(*sess).touch()
		}
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
		st, err := protocol.ParseClassicStatus(frame.Body)
		if err != nil {
			slog.Debug("status parse failed", "imei", *imei, "error", err, "body", hex.EncodeToString(frame.Body))
		} else {
			slog.Info("status", "imei", *imei, "summary", st.String(), "battery", st.BatteryPercent, "voltage_level", st.VoltageLevel, "gsm", st.GSMSignal)
			if values := st.Telemetry(); len(values) > 0 {
				s.publishTelemetry(*imei, time.Now().UTC(), values)
			}
		}
		s.replyStatus(sc, remote, *imei, frame)
	case protocol.MsgGPS, protocol.MsgGPSLBS, protocol.MsgGPS2, protocol.MsgGPSOffline, protocol.MsgAlarm:
		if *imei == "" {
			slog.Debug("GPS before login, ignoring", "remote", remote)
			return
		}
		if *sess != nil {
			(*sess).touch()
		}
		pos, err := protocol.ParseGPS(frame.Body)
		if err != nil {
			slog.Debug("GPS parse failed", "imei", *imei, "error", err, "body_len", len(frame.Body), "proto", protocol.ProtoHex(frame.Proto), "body", hex.EncodeToString(frame.Body))
		} else {
			values := map[string]any{"position_type": "gps", "latitude": pos.Latitude, "longitude": pos.Longitude, "speed": pos.SpeedKmh, "course": pos.Course, "satellites": pos.Satellites, "valid": pos.Valid}
			s.publishLocation(*imei, pos.Time, values)
			slog.Info("gps", "imei", *imei, "summary", pos.String(), "lat", pos.Latitude, "lon", pos.Longitude, "speed", pos.SpeedKmh, "course", pos.Course, "sats", pos.Satellites, "valid", pos.Valid)
		}
		// Classic GT06: no ACK for 0x12 / 0x16.
	case protocol.MsgWifiLBS, protocol.MsgWifiLBS2, protocol.MsgOfflineWifi, protocol.MsgOnlineWifi:
		if *sess != nil {
			(*sess).touch()
		}
		wl, err := protocol.ParseWifiLBS(frame.Body)
		if err != nil {
			slog.Debug("wifi/lbs parse failed", "imei", *imei, "error", err, "proto", protocol.ProtoHex(frame.Proto), "body", hex.EncodeToString(frame.Body))
		} else {
			slog.Info("wifi/lbs", "imei", *imei, "proto", protocol.ProtoHex(frame.Proto), "summary", wl.String(), "wifi_count", len(wl.Wifi), "cell_count", len(wl.Cells))
			if *imei != "" && s.geo.Enabled() {
				go s.resolveAndPublishLBS(*imei, wl)
			}
		}
	case protocol.MsgString:
		s.handleStringInfo(*imei, *sess, frame)
	default:
		if *sess != nil {
			(*sess).touch()
		}
		slog.Info("unhandled", "proto", protocol.ProtoHex(frame.Proto), "imei", *imei, "remote", remote, "serial", frame.Serial, "body_len", len(frame.Body), "body", hex.EncodeToString(frame.Body), "raw", frame.Hex())
	}
}
