package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/config"
	paho "github.com/eclipse/paho.mqtt.golang"
)

// Client is the abstraction used by the TCP server.
// It only covers the uplink path (device presence + telemetry).
// Downward RPC is an implementation detail of GatewayClient.
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
	rpc    RPCExecutor
}

// NewGatewayClient builds a real ThingsBoard Gateway MQTT client.
func NewGatewayClient(cfg config.ThingsBoardConfig) (*GatewayClient, error) {
	g := &GatewayClient{cfg: cfg}

	opts := paho.NewClientOptions()
	broker := fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port)
	opts.AddBroker(broker)
	opts.SetClientID(cfg.ClientID)
	opts.SetUsername(cfg.AccessToken)
	opts.SetPassword(cfg.Password)
	opts.SetKeepAlive(cfg.KeepAlive)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		slog.Warn("ThingsBoard MQTT connection lost", "error", err)
	})
	// Subscribe (and re-subscribe after reconnect) inside OnConnectHandler.
	opts.SetOnConnectHandler(func(c paho.Client) {
		slog.Info("ThingsBoard MQTT (re)connected")
		token := c.Subscribe("v1/gateway/rpc", cfg.QoS, g.onRPC)
		token.Wait()
		if err := token.Error(); err != nil {
			slog.Error("subscribe v1/gateway/rpc failed", "error", err)
			return
		}
		slog.Info("subscribed to v1/gateway/rpc")
	})

	g.client = paho.NewClient(opts)
	return g, nil
}

func (g *GatewayClient) SetRPCExecutor(rpc RPCExecutor) {
	g.rpc = rpc
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

// onRPC is the Paho callback for messages on v1/gateway/rpc.
func (g *GatewayClient) onRPC(_ paho.Client, msg paho.Message) {
	var req GatewayRPCRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		slog.Warn("invalid gateway RPC payload", "error", err, "raw", string(msg.Payload()))
		return
	}
	if req.Device == "" || req.Data.Method == "" {
		slog.Warn("gateway RPC missing device or method", "raw", string(msg.Payload()))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var respData any
	switch {
	case g.rpc == nil:
		respData = map[string]any{"success": false, "error": "no RPC handler configured"}
	case req.Data.Method == "":
		respData = map[string]any{"success": false, "error": "empty method"}
	default:
		if err := g.rpc.ExecuteRPC(ctx, req.Device, req.Data.Method, req.Data.Params); err != nil {
			slog.Warn("RPC failed", "device", req.Device, "method", req.Data.Method, "id", req.Data.ID, "error", err)
			respData = map[string]any{"success": false, "error": err.Error()}
		} else {
			slog.Info("RPC ok", "device", req.Device, "method", req.Data.Method, "id", req.Data.ID)
			respData = map[string]any{"success": true}
		}
	}

	resp := GatewayRPCResponse{
		Device: req.Device,
		ID:     req.Data.ID,
		Data:   respData,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		slog.Error("marshal RPC response", "error", err)
		return
	}
	if err := g.publish("v1/gateway/rpc", b); err != nil {
		slog.Error("publish RPC response", "error", err, "device", req.Device, "id", req.Data.ID)
	}
}
