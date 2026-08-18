package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/HamedFazaely/zx909-gw/internal/command"
	"github.com/HamedFazaely/zx909-gw/internal/config"
	"github.com/HamedFazaely/zx909-gw/internal/geolocation"
	"github.com/HamedFazaely/zx909-gw/internal/mqtt"
	"github.com/HamedFazaely/zx909-gw/internal/server"
)

func main() {
	cfgPath := flag.String("config", "configs/config.example.yaml", "path to config YAML")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}
	setupLogging(cfg.Logging.Level)

	// Protocol work: always use mock MQTT for now.
	tb := mqtt.NewMockClient()
	slog.Info("using mock MQTT client (device-protocol focus)")

	geo := geolocation.NewClient(cfg.Geolocation)
	if geo.Enabled() {
		slog.Info("geolocation enabled", "url", cfg.Geolocation.URL)
	} else {
		slog.Info("geolocation disabled (LBS/Wi-Fi will not publish location telemetry)")
	}

	srv := server.New(cfg.Server, tb, geo)

	// Single source of truth for device commands (debug REST + future MQTT RPC).
	cmdHandler := command.NewHandler(srv, slog.Default())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.Server.DebugAPI != "" {
		go func() {
			if err := server.ListenAndServeDebug(cfg.Server.DebugAPI, srv, cmdHandler); err != nil {
				slog.Error("debug API stopped", "error", err)
			}
		}()
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("TCP server stopped", "error", err)
			stop()
		}
	}()
	slog.Info("gateway running", "tcp", cfg.Server.Listen, "debug_api", cfg.Server.DebugAPI)

	<-ctx.Done()
	slog.Info("shutting down…")
	srv.Shutdown()
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(h))
}
