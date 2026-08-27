package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/command"
)

type DebugAPI struct {
	srv     *Server
	handler *command.Handler
}

func NewDebugAPI(srv *Server, handler *command.Handler) *DebugAPI {
	return &DebugAPI{srv: srv, handler: handler}
}

func (d *DebugAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /devices", d.listDevices)
	mux.HandleFunc("POST /devices/{imei}/reboot", d.reboot)
	mux.HandleFunc("POST /devices/{imei}/shutdown", d.shutdown)
	mux.HandleFunc("POST /devices/{imei}/interval", d.interval)
	mux.HandleFunc("POST /devices/{imei}/locate", d.locate)
	mux.HandleFunc("POST /devices/{imei}/find", d.find)
	mux.HandleFunc("POST /devices/{imei}/command", d.command)
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
	IdleSeconds     *uint16 `json:"idle_seconds"`
	StatusMinutes   *uint8  `json:"status_minutes"`
}

func (d *DebugAPI) interval(w http.ResponseWriter, r *http.Request) {
	imei := r.PathValue("imei")
	var req intervalReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.LocationSeconds == nil && req.IdleSeconds == nil && req.StatusMinutes == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provide location_seconds, idle_seconds, and/or status_minutes"})
		return
	}

	sent := map[string]any{}

	if req.LocationSeconds != nil || req.IdleSeconds != nil {
		on := 60
		if req.LocationSeconds != nil {
			on = int(*req.LocationSeconds)
		}
		idle := on
		if req.IdleSeconds != nil {
			idle = int(*req.IdleSeconds)
		}
		params, _ := json.Marshal(map[string]int{"seconds": on, "idle_seconds": idle})
		if err := d.handler.ExecuteRPC(r.Context(), imei, "setLocationInterval", params); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		sent["location_seconds"] = on
		sent["idle_seconds"] = idle
	}
	if req.StatusMinutes != nil {
		params, _ := json.Marshal(map[string]int{"minutes": int(*req.StatusMinutes)})
		if err := d.handler.ExecuteRPC(r.Context(), imei, "setStatusInterval", params); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		sent["status_minutes"] = *req.StatusMinutes
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imei": imei, "sent": sent})
}

func (d *DebugAPI) locate(w http.ResponseWriter, r *http.Request) {
	imei := r.PathValue("imei")
	if err := d.handler.ExecuteRPC(r.Context(), imei, "locate", nil); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imei": imei, "cmd": "locate"})
}

func (d *DebugAPI) find(w http.ResponseWriter, r *http.Request) {
	imei := r.PathValue("imei")
	var req struct {
		On *bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.On == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provide on: true|false"})
		return
	}
	method := "findOff"
	if *req.On {
		method = "findOn"
	}
	if err := d.handler.ExecuteRPC(r.Context(), imei, method, nil); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imei": imei, "cmd": method, "on": *req.On})
}

func (d *DebugAPI) command(w http.ResponseWriter, r *http.Request) {
	imei := r.PathValue("imei")
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
		return
	}
	params, _ := json.Marshal(map[string]string{"text": req.Text})
	if err := d.handler.ExecuteRPC(r.Context(), imei, "send", params); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imei": imei, "cmd": "send", "text": req.Text})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func ListenAndServeDebug(addr string, srv *Server, handler *command.Handler) error {
	api := NewDebugAPI(srv, handler)
	slog.Info("debug REST listening", "addr", addr,
		"endpoints", strings.Join([]string{
			"GET /devices",
			"POST /devices/{imei}/reboot",
			"POST /devices/{imei}/shutdown",
			"POST /devices/{imei}/interval",
			"POST /devices/{imei}/locate",
			"POST /devices/{imei}/find",
			"POST /devices/{imei}/command",
		}, ", "))
	return http.ListenAndServe(addr, api.Handler())
}
