package server

import (
	"encoding/hex"
	"log/slog"

	"github.com/HamedFazaely/zx909-gw/internal/protocol"
)

func (s *Server) handleStringInfo(imei string, sess *Session, frame *protocol.Frame) {
	if sess != nil {
		sess.touch()
	}
	info, err := protocol.ParseClassicString(frame.Body)
	if err != nil {
		slog.Debug("string parse failed", "imei", imei, "error", err, "classic", frame.Classic, "body", hex.EncodeToString(frame.Body))
		return
	}
	slog.Info("string", "imei", imei, "text", info.Text, "has_pos", info.HasPos, "lat", info.Lat, "lon", info.Lon, "flag", info.Flag)
	values := info.Telemetry()
	if len(values) == 0 {
		return
	}
	if info.HasPos {
		s.publishLocation(imei, info.Time, values)
		return
	}
	s.publishTelemetry(imei, info.Time, values)
}
