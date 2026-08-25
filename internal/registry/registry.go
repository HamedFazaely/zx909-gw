package registry

import "context"

// Registry decides whether a tracker IMEI may use the ThingsBoard uplink path.
// Protocol ACKs always proceed; only ConnectDevice / telemetry / disconnect are gated.
type Registry interface {
	// Enabled reports whether registration checks are active.
	Enabled() bool
	// IsRegistered returns true if TB uplink is allowed for imei.
	// Implementations should fail closed (false) on remote errors when enabled.
	IsRegistered(ctx context.Context, imei string) bool
}

// AllowAll treats every IMEI as registered (lab / paapeli.enabled=false).
type AllowAll struct{}

func (AllowAll) Enabled() bool { return false }

func (AllowAll) IsRegistered(context.Context, string) bool { return true }
