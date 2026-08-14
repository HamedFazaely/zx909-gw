package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/HamedFazaely/zx909-gw/internal/config"
)

type Client interface {
	Connect(ctx context.Context) error
	Close()
	ConnectDevice(deviceName string) error
	DisconnectDevice(deviceName string) error 
	PublishTelemetry(deviceName string, ts time.Time, values map[string]any) error
}

// GatewayClient talks to ThingsBoard using the MQTT Gateway API.
type GatewayClient struct {
	cfg    config.ThingsBoardConfig
	client paho.Client
}

func NewGatewayClient(cfg config.ThingsBoardConfig) (*GatewayClient, error) {
	opts := paho.NewClientOptions()
	broker := fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port)
	opts.AddBroker(broker)
	opts.SetClientID(cfg.ClientID)
	opts.SetUsername(cfg.AccessToken)
	opts.SetKeepAlive(cfg.KeepAlive)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		slog.Warn("ThingsBoard MQTT connection lost", "error", err)
	})
	opts.SetOnConnectHandler(func(_ paho.Client) {
		slog.Info("ThingsBoard MQTT (re)connected")
	})

	c := paho.NewClient(opts)
	return &GatewayClient{cfg: cfg, client: c}, nil
}

func (g *GatewayClient) Connect(ctx context.Context) error {
	token := g.client.Connect()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-token.Done():
		if token.Error() != nil {
			return token.Error()
		}
	}
	return nil
}

func (g *GatewayClient) Close() {
	g.client.Disconnect(250)
}

// ConnectDevice announces a child device to ThingsBoard.
func (g *GatewayClient) ConnectDevice(deviceName string) error {
	payload := map[string]string{
		"device": deviceName,
		"type":   g.cfg.DeviceProfile,
	}
	b, _ := json.Marshal(payload)
	return g.publish("v1/gateway/connect", b)
}

// DisconnectDevice announces a child device went offline.
func (g *GatewayClient) DisconnectDevice(deviceName string) error {
	payload := map[string]string{"device": deviceName}
	b, _ := json.Marshal(payload)
	return g.publish("v1/gateway/disconnect", b)
}

// PublishTelemetry sends one telemetry sample for a device.
func (g *GatewayClient) PublishTelemetry(deviceName string, ts time.Time, values map[string]any) error {
	entry := map[string]any{
		"ts":     ts.UnixMilli(),
		"values": values,
	}
	payload := map[string]any{
		deviceName: []any{entry},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return g.publish("v1/gateway/telemetry", b)
}

func (g *GatewayClient) publish(topic string, payload []byte) error {
	token := g.client.Publish(topic, g.cfg.QoS, false, payload)
	token.Wait()
	return token.Error()
}
