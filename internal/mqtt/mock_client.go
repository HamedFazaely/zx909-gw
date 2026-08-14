package mqtt

import (
	"context"
	"log/slog"
	"time"
)

// MockClient implements Client without talking to a real MQTT broker.
// Useful while developing / debugging the device protocol side.
type MockClient struct{}

func NewMockClient() *MockClient {
	return &MockClient{}
}

func (m *MockClient) Connect(ctx context.Context) error {
	slog.Info("mqtt mock: Connect called (no-op)")
	return nil
}

func (m *MockClient) Close() {
	slog.Info("mqtt mock: Close called (no-op)")
}

func (m *MockClient) ConnectDevice(deviceName string) error {
	slog.Info("mqtt mock: ConnectDevice",
		"device", deviceName,
		"topic", "v1/gateway/connect",
	)
	return nil
}

func (m *MockClient) DisconnectDevice(deviceName string) error {
	slog.Info("mqtt mock: DisconnectDevice",
		"device", deviceName,
		"topic", "v1/gateway/disconnect",
	)
	return nil
}

func (m *MockClient) PublishTelemetry(deviceName string, ts time.Time, values map[string]any) error {
	slog.Info("mqtt mock: PublishTelemetry",
		"device", deviceName,
		"topic", "v1/gateway/telemetry",
		"ts", ts.UTC().Format(time.RFC3339),
		"values", values,
	)
	return nil
}
