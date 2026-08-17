package mqtt

import (
	"context"
	"encoding/json"
)

// RPCExecutor is the narrow interface the real MQTT client needs in order to
// turn a ThingsBoard Gateway RPC into a binary command on a live TCP session.
// *command.Handler satisfies this interface.
type RPCExecutor interface {
	ExecuteRPC(ctx context.Context, imei, method string, params json.RawMessage) error
}

// GatewayRPCRequest is the payload ThingsBoard publishes on v1/gateway/rpc.
type GatewayRPCRequest struct {
	Device string `json:"device"`
	Data   struct {
		ID     int             `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	} `json:"data"`
}

// GatewayRPCResponse is published back on v1/gateway/rpc so ThingsBoard can
// correlate the reply with the original request via the id field.
type GatewayRPCResponse struct {
	Device string `json:"device"`
	ID     int    `json:"id"`
	Data   any    `json:"data"` // {"success": true} or {"success": false, "error": "..."}
}
