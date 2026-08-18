package geolocation

import (
	"context"
	"fmt"
)

// WifiAP is one observed access point for geolocation.
type WifiAP struct {
	MAC  string // "aa:bb:cc:dd:ee:ff"
	RSSI int    // dBm or device raw signal; provider-dependent
}

// CellTower is one observed cell for geolocation.
type CellTower struct {
	MCC    int
	MNC    int
	LAC    int
	CellID int64
	Signal int
}

// Request holds radio observations to resolve into coordinates.
type Request struct {
	Wifi  []WifiAP
	Cells []CellTower
}

// Result is a resolved position from a geolocation provider.
type Result struct {
	Latitude  float64
	Longitude float64
	Accuracy  float64 // metres; 0 if unknown
}

// Client resolves LBS/Wi-Fi observations to lat/lon.
// Implementations must be safe for concurrent use.
type Client interface {
	// Enabled reports whether Locate should be invoked (config flag).
	Enabled() bool
	Locate(ctx context.Context, req Request) (*Result, error)
}

// Disabled is a no-op client used when geolocation is turned off.
type Disabled struct{}

func (Disabled) Enabled() bool { return false }

func (Disabled) Locate(context.Context, Request) (*Result, error) {
	return nil, fmt.Errorf("geolocation disabled")
}
