package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/etc/hostoriagp/config.yml"

// Config is the Hostoria Node configuration, stored at DefaultConfigPath.
// The billing panel writes this file via the `configure` subcommand.
type Config struct {
	Debug   bool   `yaml:"debug"    json:"debug"`
	UUID    string `yaml:"uuid"     json:"uuid"`
	TokenID string `yaml:"token_id" json:"token_id"`
	Token   string `yaml:"token"    json:"token"`
	API     API    `yaml:"api"      json:"api"`
	System  System `yaml:"system"   json:"system"`
	Remote  string `yaml:"remote"   json:"remote"`
}

type API struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
	SSL  SSL    `yaml:"ssl"  json:"ssl"`
}

type SSL struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Cert    string `yaml:"cert"    json:"cert"`
	Key     string `yaml:"key"     json:"key"`
}

type System struct {
	Data string   `yaml:"data" json:"data"`
	SFTP SFTPConf `yaml:"sftp" json:"sftp"`
}

type SFTPConf struct {
	BindAddress string `yaml:"bind_address" json:"bind_address"`
	BindPort    int    `yaml:"bind_port"    json:"bind_port"`
	ReadOnly    bool   `yaml:"read_only"   json:"read_only"`
	KeyPath     string `yaml:"key_path"    json:"key_path"`
}

var instance *Config

// Load reads and parses the config file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	applyDefaults(&cfg)
	instance = &cfg
	return &cfg, nil
}

// Save marshals cfg to YAML and writes it to path.
func Save(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(path, data, 0640)
}

// Get returns the globally loaded config instance (set by Load).
func Get() *Config {
	return instance
}

func applyDefaults(cfg *Config) {
	if cfg.API.Host == "" {
		cfg.API.Host = "0.0.0.0"
	}
	if cfg.API.Port == 0 {
		cfg.API.Port = 8989
	}
	if cfg.System.Data == "" {
		cfg.System.Data = "/var/lib/hostoria"
	}
	if cfg.System.SFTP.BindAddress == "" {
		cfg.System.SFTP.BindAddress = "0.0.0.0"
	}
	if cfg.System.SFTP.BindPort == 0 {
		cfg.System.SFTP.BindPort = 2022
	}
}
