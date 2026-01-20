package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// ErrNoConfig is returned when no config file is found.
var ErrNoConfig = errors.New("no cinch config file found")

// Config is the parsed cinch configuration.
type Config struct {
	// Command is the shell command to run (required).
	Command string `yaml:"command" toml:"command" json:"command"`

	// Workers is a list of worker labels to fan-out to.
	// If empty, runs on any available worker.
	Workers []string `yaml:"workers" toml:"workers" json:"workers"`

	// Timeout for the job. Default: 30m.
	Timeout Duration `yaml:"timeout" toml:"timeout" json:"timeout"`

	// Services are containers started before the build.
	Services map[string]Service `yaml:"services" toml:"services" json:"services"`

	// Container overrides auto-detection. Optional.
	// Set to "none" for bare metal.
	Container string `yaml:"container" toml:"container" json:"container"`
}

// Service is a container that runs alongside the build.
type Service struct {
	Image       string            `yaml:"image" toml:"image" json:"image"`
	Env         map[string]string `yaml:"env" toml:"env" json:"env"`
	Command     string            `yaml:"command" toml:"command" json:"command"`
	Healthcheck *Healthcheck      `yaml:"healthcheck" toml:"healthcheck" json:"healthcheck"`
}

// Healthcheck configures how to check if a service is ready.
type Healthcheck struct {
	Cmd     string   `yaml:"cmd" toml:"cmd" json:"cmd"`
	Timeout Duration `yaml:"timeout" toml:"timeout" json:"timeout"`
}

// Duration wraps time.Duration for custom parsing.
type Duration time.Duration

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

func (d *Duration) UnmarshalText(text []byte) error {
	dur, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(text), err)
	}
	*d = Duration(dur)
	return nil
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// Load finds and parses a cinch config file from the given directory.
func Load(dir string) (*Config, string, error) {
	candidates := []struct {
		name   string
		parser func([]byte, *Config) error
	}{
		{".cinch.yaml", parseYAML},
		{".cinch.yml", parseYAML},
		{".cinch.toml", parseTOML},
		{".cinch.json", parseJSON},
		{"cinch.yaml", parseYAML},
		{"cinch.yml", parseYAML},
		{"cinch.toml", parseTOML},
		{"cinch.json", parseJSON},
	}

	for _, c := range candidates {
		path := filepath.Join(dir, c.name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // File doesn't exist, try next
		}

		var cfg Config
		if err := c.parser(data, &cfg); err != nil {
			return nil, c.name, fmt.Errorf("parse %s: %w", c.name, err)
		}

		if err := cfg.Validate(); err != nil {
			return nil, c.name, fmt.Errorf("validate %s: %w", c.name, err)
		}

		// Apply defaults
		cfg.applyDefaults()

		return &cfg, c.name, nil
	}

	return nil, "", ErrNoConfig
}

func parseYAML(data []byte, cfg *Config) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true) // Strict: error on unknown fields
	return decoder.Decode(cfg)
}

func parseTOML(data []byte, cfg *Config) error {
	_, err := toml.Decode(string(data), cfg)
	return err
}

func parseJSON(data []byte, cfg *Config) error {
	return json.Unmarshal(data, cfg)
}

// Validate checks the config for errors.
func (c *Config) Validate() error {
	if c.Command == "" {
		return errors.New("command is required")
	}

	// Check for YAML footguns
	if c.Command == "true" || c.Command == "false" {
		return errors.New("command looks like a boolean - did YAML mangle it? Quote your command")
	}

	// Validate services
	for name, svc := range c.Services {
		if svc.Image == "" {
			return fmt.Errorf("service %q: image is required", name)
		}
	}

	return nil
}

func (c *Config) applyDefaults() {
	if c.Timeout == 0 {
		c.Timeout = Duration(30 * time.Minute)
	}

	for name, svc := range c.Services {
		if svc.Healthcheck != nil && svc.Healthcheck.Timeout == 0 {
			svc.Healthcheck.Timeout = Duration(60 * time.Second)
		}
		c.Services[name] = svc
	}
}

// IsBareMetalContainer returns true if container is explicitly set to "none".
func (c *Config) IsBareMetalContainer() bool {
	return c.Container == "none"
}
