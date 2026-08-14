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
	Logging     LoggingConfig     `yaml:"logging"`
}

type ServerConfig struct {
	Listen       string        `yaml:"listen"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type ThingsBoardConfig struct {
	Host          string        `yaml:"host"`
	Port          int           `yaml:"port"`
	AccessToken   string        `yaml:"access_token"`
	ClientID      string        `yaml:"client_id"`
	DeviceProfile string        `yaml:"device_profile"`
	QoS           byte          `yaml:"qos"`
	KeepAlive     time.Duration `yaml:"keepalive"`
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

	// Defaults
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
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}

	// ThingsBoard credentials are only required when using the real MQTT client.
	// While developing the device protocol side with the mock they can be left empty.

	return &cfg, nil
}
