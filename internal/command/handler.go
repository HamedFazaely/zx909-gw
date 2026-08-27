package command

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/HamedFazaely/zx909-gw/internal/protocol"
)

type SessionLookup interface {
	SendToDevice(imei string, payload []byte) error
	NextCommandSerial(imei string) uint16
}

type Handler struct {
	sessions SessionLookup
	log      *slog.Logger
}

func NewHandler(sessions SessionLookup, log *slog.Logger) *Handler {
	return &Handler{sessions: sessions, log: log}
}

func (h *Handler) packet(imei, method string, params json.RawMessage) ([]byte, error) {
	serial := h.sessions.NextCommandSerial(imei)

	switch method {
	case "reboot":
		return protocol.BuildClassicRestart(serial), nil
	case "shutdown":
		return protocol.BuildClassicShutdown(serial), nil
	case "locate":
		return protocol.BuildClassicLocate(serial), nil
	case "findOn":
		return protocol.BuildClassicFind(true, serial), nil
	case "findOff":
		return protocol.BuildClassicFind(false, serial), nil
	case "send":
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params for send: %w", err)
		}
		text := strings.TrimSpace(p.Text)
		if text == "" || len(text) > 80 {
			return nil, fmt.Errorf("text must be 1-80 characters")
		}
		for _, r := range text {
			if r < 0x20 || r > 0x7E || !unicode.IsPrint(r) {
				return nil, fmt.Errorf("text must be printable ASCII")
			}
		}
		return protocol.BuildClassicCommand(text, serial), nil
	case "setLocationInterval":
		var p struct {
			Seconds     int `json:"seconds"`
			IdleSeconds int `json:"idle_seconds"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params for setLocationInterval: %w", err)
		}
		if p.Seconds < 5 || p.Seconds > 18000 {
			return nil, fmt.Errorf("seconds out of range (5-18000)")
		}
		idle := p.IdleSeconds
		if idle == 0 {
			idle = p.Seconds
		}
		if idle < 5 || idle > 18000 {
			return nil, fmt.Errorf("idle_seconds out of range (5-18000)")
		}
		return protocol.BuildClassicTimer(p.Seconds, idle, serial), nil
	case "setStatusInterval":
		var p struct {
			Seconds     int `json:"seconds"`
			IdleSeconds int `json:"idle_seconds"`
			Minutes     int `json:"minutes"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params for setStatusInterval: %w", err)
		}
		on := p.Seconds
		if on == 0 && p.Minutes > 0 {
			on = p.Minutes * 60
		}
		if on < 60 || on > 18000 {
			return nil, fmt.Errorf("heartbeat seconds out of range (60-18000)")
		}
		idle := p.IdleSeconds
		if idle == 0 {
			idle = on
		}
		if idle < 60 || idle > 18000 {
			return nil, fmt.Errorf("heartbeat idle_seconds out of range (60-18000)")
		}
		return protocol.BuildClassicHeartbeat(on, idle, serial), nil
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

func (h *Handler) ExecuteRPC(ctx context.Context, imei, method string, params json.RawMessage) error {
	pkt, err := h.packet(imei, method, params)
	if err != nil {
		return err
	}
	h.log.Info("command", "imei", imei, "method", method, "hex", fmt.Sprintf("%x", pkt))
	return h.sessions.SendToDevice(imei, pkt)
}

func (h *Handler) ApplySharedAttributes(ctx context.Context, imei string, attrs map[string]any) error {
	if v, ok := attrs["locationIntervalSeconds"]; ok {
		if sec, ok := toInt(v); ok && sec >= 5 && sec <= 18000 {
			params, _ := json.Marshal(map[string]int{"seconds": sec})
			if err := h.ExecuteRPC(ctx, imei, "setLocationInterval", params); err != nil {
				return err
			}
		}
	}
	if v, ok := attrs["statusIntervalMinutes"]; ok {
		if min, ok := toInt(v); ok && min >= 1 && min <= 60 {
			params, _ := json.Marshal(map[string]int{"minutes": min})
			if err := h.ExecuteRPC(ctx, imei, "setStatusInterval", params); err != nil {
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
