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
	IsClassic(imei string) bool
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
	classic := h.sessions.IsClassic(imei)
	serial := uint16(1)
	if classic {
		serial = h.sessions.NextCommandSerial(imei)
	}

	switch method {
	case "reboot":
		if classic {
			return protocol.BuildClassicRestart(serial), nil
		}
		return protocol.BuildRestart(), nil
	case "shutdown":
		if classic {
			return protocol.BuildClassicShutdown(serial), nil
		}
		return protocol.BuildShutdown(), nil
	case "locate":
		if classic {
			return protocol.BuildClassicLocate(serial), nil
		}
		return protocol.BuildLocate(), nil
	case "findOn":
		if !classic {
			return nil, fmt.Errorf("findOn is classic GT06 only")
		}
		return protocol.BuildClassicFind(true, serial), nil
	case "findOff":
		if !classic {
			return nil, fmt.Errorf("findOff is classic GT06 only")
		}
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
		if !classic {
			return nil, fmt.Errorf("raw ASCII send is only supported on classic GT06 sessions")
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
		if classic {
			return protocol.BuildClassicTimer(p.Seconds, idle, serial), nil
		}
		if p.Seconds < 10 || p.Seconds > 7200 {
			return nil, fmt.Errorf("seconds out of range (10-7200)")
		}
		return protocol.BuildUploadInterval(uint16(p.Seconds)), nil
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
		if classic {
			return protocol.BuildClassicHeartbeat(on, idle, serial), nil
		}
		if p.Minutes < 1 || p.Minutes > 60 {
			return nil, fmt.Errorf("minutes out of range (1-60)")
		}
		return protocol.BuildStatusInterval(p.Minutes), nil
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

func (h *Handler) ExecuteRPC(ctx context.Context, imei, method string, params json.RawMessage) error {
	pkt, err := h.packet(imei, method, params)
	if err != nil {
		return err
	}
	h.log.Info("command", "imei", imei, "method", method, "classic", h.sessions.IsClassic(imei), "hex", fmt.Sprintf("%x", pkt))
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
