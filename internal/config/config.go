package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	ThingsBoard ThingsBoardConfig `yaml:"thingsboard"`
	Geolocation GeolocationConfig `yaml:"geolocation"`
	Paapeli     PaapeliConfig     `yaml:"paapeli"`
	Logging     LoggingConfig     `yaml:"logging"`
}

type ServerConfig struct {
	Listen       string        `yaml:"listen"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	// DebugAPI is an optional HTTP bind address for the command injector
	// (e.g. "127.0.0.1:8090"). Empty disables it.
	DebugAPI string `yaml:"debug_api"`
}

type ThingsBoardConfig struct {
	Host          string        `yaml:"host"`
	Port          int           `yaml:"port"`
	AccessToken   string        `yaml:"access_token"`
	ClientID      string        `yaml:"client_id"`
	Password      string        `yaml:"password"`
	DeviceProfile string        `yaml:"device_profile"`
	QoS           byte          `yaml:"qos"`
	KeepAlive     time.Duration `yaml:"keepalive"`
	UseMock       bool          `yaml:"use_mock"`
}

// GeolocationConfig controls optional LBS/Wi-Fi → lat/lon resolution.
type GeolocationConfig struct {
	Enabled bool          `yaml:"enabled"`
	URL     string        `yaml:"url"`
	APIKey  string        `yaml:"api_key"`
	Timeout time.Duration `yaml:"timeout"`
}

// PaapeliConfig controls claim-before-uplink registration checks.
type PaapeliConfig struct {
	Enabled     bool          `yaml:"enabled"`
	BaseURL     string        `yaml:"base_url"`
	Username    string        `yaml:"username"`
	Password    string        `yaml:"password"`
	Timeout     time.Duration `yaml:"timeout"`
	PositiveTTL time.Duration `yaml:"positive_ttl"` // cache TTL when registered=true
	NegativeTTL time.Duration `yaml:"negative_ttl"` // cache TTL when registered=false
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":8002"
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 5 * time.Minute
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 10 * time.Second
	}
	if cfg.ThingsBoard.Port == 0 {
		cfg.ThingsBoard.Port = 1883
	}
	if cfg.ThingsBoard.DeviceProfile == "" {
		cfg.ThingsBoard.DeviceProfile = "pet-tracker"
	}
	if cfg.ThingsBoard.QoS == 0 {
		cfg.ThingsBoard.QoS = 1
	}
	if cfg.ThingsBoard.KeepAlive == 0 {
		cfg.ThingsBoard.KeepAlive = 30 * time.Second
	}
	if cfg.ThingsBoard.ClientID == "" {
		cfg.ThingsBoard.ClientID = "zx909-gw"
	}
	if cfg.Geolocation.Timeout == 0 {
		cfg.Geolocation.Timeout = 3 * time.Second
	}
	if cfg.Paapeli.Timeout == 0 {
		cfg.Paapeli.Timeout = 3 * time.Second
	}
	if cfg.Paapeli.PositiveTTL == 0 {
		cfg.Paapeli.PositiveTTL = 30 * time.Minute
	}
	if cfg.Paapeli.NegativeTTL == 0 {
		cfg.Paapeli.NegativeTTL = 60 * time.Second
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}

	return &cfg, nil
}
