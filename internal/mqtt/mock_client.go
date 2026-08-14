package mqtt

import (
	"context"
	"log/slog"
	"time"
)

type MockClient struct{}

func (m *MockClient) Connect(ctx context.Context) error {
	return nil
}

func (m *MockClient) Close() {

}

func (m *MockClient) ConnectDevice(deviceName string) error {
	slog.Info("Connecting device", deviceName, "")
	return nil
}

func (m *MockClient) DisconnectDevice(deviceName string) error {
	slog.Info("Disconnect device", deviceName, "")
	return nil
}

func (m *MockClient) PublishTelemetry(deviceName string, ts time.Time, values map[string]any) error {
	slog.Info("Publishing telemetry", deviceName, values)
	return nil
}
