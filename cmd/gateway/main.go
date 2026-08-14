package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/HamedFazaely/zx909-gw/internal/config"
	"github.com/HamedFazaely/zx909-gw/internal/mqtt"
	"github.com/HamedFazaely/zx909-gw/internal/server"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	setupLogging(cfg.Logging.Level)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Device-protocol focus: use mock MQTT client ---
	// Swap back to mqtt.NewGatewayClient(cfg.ThingsBoard) when ready for real TB.
	tbClient := mqtt.NewMockClient()
	if err := tbClient.Connect(ctx); err != nil {
		slog.Error("failed to connect MQTT client", "error", err)
		os.Exit(1)
	}
	defer tbClient.Close()
	slog.Info("using mock MQTT client (protocol-debug mode)")

	srv := server.New(cfg.Server, tbClient)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("TCP server stopped", "error", err)
			stop()
		}
	}()
	slog.Info("TCP server listening", "addr", cfg.Server.Listen)

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
