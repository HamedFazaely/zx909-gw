package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/command"
	"github.com/HamedFazaely/zx909-gw/internal/protocol"
)

// DebugAPI is a localhost-oriented HTTP surface for injecting downlink
// commands while reverse-engineering the device protocol. Not for production
// exposure without auth.
type DebugAPI struct {
	srv     *Server
	handler *command.Handler
}

func NewDebugAPI(srv *Server) *DebugAPI {
	return &DebugAPI{srv: srv, handler: command.NewHandler(srv, &slog.Logger{})}
}

func (d *DebugAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /devices", d.listDevices)
	mux.HandleFunc("POST /devices/{imei}/reboot", d.reboot)
	mux.HandleFunc("POST /devices/{imei}/shutdown", d.shutdown)
	mux.HandleFunc("POST /devices/{imei}/interval", d.interval)
	mux.HandleFunc("POST /devices/{imei}/locate", d.locate)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ts": time.Now().UTC()})
	})
	return mux
}

func (d *DebugAPI) listDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": d.srv.ListSessions()})
}

func (d *DebugAPI) reboot(w http.ResponseWriter, r *http.Request) {
	imei := r.PathValue("imei")
	if err := d.handler.ExecuteRPC(r.Context(), imei, "reboot", nil); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imei": imei, "cmd": "reboot"})
}

func (d *DebugAPI) shutdown(w http.ResponseWriter, r *http.Request) {
	imei := r.PathValue("imei")
	if err := d.handler.ExecuteRPC(r.Context(), imei, "shutdown", nil); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imei": imei, "cmd": "shutdown"})
}

type intervalReq struct {
	LocationSeconds *uint16 `json:"location_seconds"`
	StatusMinutes   *uint8  `json:"status_minutes"`
}

func (d *DebugAPI) interval(w http.ResponseWriter, r *http.Request) {
	imei := r.PathValue("imei")
	var req intervalReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.LocationSeconds == nil && req.StatusMinutes == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provide location_seconds and/or status_minutes"})
		return
	}
	sent := map[string]any{}
	if req.LocationSeconds != nil {
		frame := protocol.BuildUploadInterval(*req.LocationSeconds)
		if err := d.srv.SendToDevice(imei, frame); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		sent["location_seconds"] = *req.LocationSeconds
	}
	if req.StatusMinutes != nil {
		frame := protocol.BuildStatusInterval(int(*req.StatusMinutes))
		if err := d.srv.SendToDevice(imei, frame); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		sent["status_minutes"] = *req.StatusMinutes
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imei": imei, "sent": sent})
}

func (d *DebugAPI) locate(w http.ResponseWriter, r *http.Request) {
	imei := r.PathValue("imei")
	if err := d.srv.SendToDevice(imei, protocol.BuildLocate()); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imei": imei, "cmd": "locate"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ListenAndServeDebug starts the debug HTTP server. Blocks until error.
func ListenAndServeDebug(addr string, srv *Server) error {
	api := NewDebugAPI(srv)
	slog.Info("debug REST listening", "addr", addr,
		"endpoints", strings.Join([]string{
			"GET /devices",
			"POST /devices/{imei}/reboot",
			"POST /devices/{imei}/shutdown",
			"POST /devices/{imei}/interval",
			"POST /devices/{imei}/locate",
		}, ", "))
	return http.ListenAndServe(addr, api.Handler())
}
