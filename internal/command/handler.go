package command

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/HamedFazaely/zx909-gw/internal/protocol"
)

// SessionLookup is implemented by the TCP server.
// It returns an error if the device is currently offline.
type SessionLookup interface {
	SendToDevice(imei string, payload []byte) error
}

// Handler turns high-level commands (RPC or attribute changes)
// into the binary packets the trackers understand.
type Handler struct {
	sessions SessionLookup
	log      *slog.Logger
}

func NewHandler(sessions SessionLookup, log *slog.Logger) *Handler {
	return &Handler{sessions: sessions, log: log}
}

// ExecuteRPC is the single entry point used by both the MQTT path
// and the debug REST API.
func (h *Handler) ExecuteRPC(ctx context.Context, imei, method string, params json.RawMessage) error {
	var pkt []byte

	switch method {
	case "reboot":
		pkt = protocol.BuildRestart()
	case "shutdown":
		pkt = protocol.BuildShutdown()
	case "locate":
		pkt = protocol.BuildLocate()
	case "setLocationInterval":
		var p struct {
			Seconds int `json:"seconds"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return fmt.Errorf("invalid params for setLocationInterval: %w", err)
		}
		if p.Seconds < 10 || p.Seconds > 7200 {
			return fmt.Errorf("seconds out of range (10-7200)")
		}
		pkt = protocol.BuildUploadInterval(uint16(p.Seconds))
	case "setStatusInterval":
		var p struct {
			Minutes int `json:"minutes"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return fmt.Errorf("invalid params for setStatusInterval: %w", err)
		}
		if p.Minutes < 1 || p.Minutes > 60 {
			return fmt.Errorf("minutes out of range (1-60)")
		}
		pkt = protocol.BuildStatusInterval(p.Minutes)
	default:
		return fmt.Errorf("unknown method %q", method)
	}

	h.log.Info("command", "imei", imei, "method", method)
	return h.sessions.SendToDevice(imei, pkt)
}

// ApplySharedAttributes applies durable configuration that arrived
// as shared-attribute updates. Safe to call even if the device is offline
// (the error is returned so the caller can decide).
func (h *Handler) ApplySharedAttributes(ctx context.Context, imei string, attrs map[string]any) error {
	if v, ok := attrs["locationIntervalSeconds"]; ok {
		if sec, ok := toInt(v); ok && sec >= 10 && sec <= 7200 {
			if err := h.sessions.SendToDevice(imei, protocol.BuildUploadInterval(uint16(sec))); err != nil {
				return err
			}
		}
	}
	if v, ok := attrs["statusIntervalMinutes"]; ok {
		if min, ok := toInt(v); ok && min >= 1 && min <= 60 {
			if err := h.sessions.SendToDevice(imei, protocol.BuildStatusInterval(min)); err != nil {
				return err
			}
		}
	}
	return nil
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}