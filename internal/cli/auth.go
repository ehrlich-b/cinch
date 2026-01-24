package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// CLIConfig represents the CLI configuration file (~/.cinch/config).
type CLIConfig struct {
	Servers map[string]ServerConfig `toml:"servers"`
}

// ServerConfig represents a server configuration.
type ServerConfig struct {
	URL   string `toml:"url"`
	Token string `toml:"token"`
	User  string `toml:"user"`
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cinch", "config")
}

// LoadConfig loads the CLI config from disk.
func LoadConfig() (*CLIConfig, error) {
	path := DefaultConfigPath()
	if path == "" {
		return &CLIConfig{Servers: make(map[string]ServerConfig)}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CLIConfig{Servers: make(map[string]ServerConfig)}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg CLIConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Servers == nil {
		cfg.Servers = make(map[string]ServerConfig)
	}

	return &cfg, nil
}

// SaveConfig saves the CLI config to disk.
func SaveConfig(cfg *CLIConfig) error {
	path := DefaultConfigPath()
	if path == "" {
		return fmt.Errorf("cannot determine config path")
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	// Write to temp file first
	tmpFile := path + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}

	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("write config: %w", err)
	}
	f.Close()

	// Atomic rename
	if err := os.Rename(tmpFile, path); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("save config: %w", err)
	}

	return nil
}

// GetServerConfig returns the config for a server, or nil if not found.
func (c *CLIConfig) GetServerConfig(serverURL string) *ServerConfig {
	for _, sc := range c.Servers {
		if sc.URL == serverURL {
			return &sc
		}
	}
	return nil
}

// SetServerConfig sets or updates a server config.
func (c *CLIConfig) SetServerConfig(name string, sc ServerConfig) {
	c.Servers[name] = sc
}

// DeviceAuthResponse is the response from POST /auth/device.
type DeviceAuthResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// DeviceTokenResponse is the response from POST /auth/device/token.
type DeviceTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	User        string `json:"user"`
	Error       string `json:"error"`
}

// RequestDeviceCode initiates the device authorization flow.
func RequestDeviceCode(serverURL string) (*DeviceAuthResponse, error) {
	url := serverURL + "/auth/device"

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var result DeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// PollForToken polls for the device token until authorized or expired.
func PollForToken(serverURL, deviceCode string, interval int) (*DeviceTokenResponse, error) {
	url := serverURL + "/auth/device/token"

	client := &http.Client{Timeout: 10 * time.Second}

	for {
		body := fmt.Sprintf(`{"device_code":"%s"}`, deviceCode)
		req, err := http.NewRequest("POST", url, io.NopCloser(
			&readerString{s: body},
		))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		var result DeviceTokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()

		if result.Error == "" && result.AccessToken != "" {
			return &result, nil
		}

		if result.Error == "authorization_pending" {
			time.Sleep(time.Duration(interval) * time.Second)
			continue
		}

		return nil, fmt.Errorf("authorization failed: %s", result.Error)
	}
}

// readerString is a simple io.Reader for a string.
type readerString struct {
	s   string
	pos int
}

func (r *readerString) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.s) {
		return 0, io.EOF
	}
	n = copy(p, r.s[r.pos:])
	r.pos += n
	return n, nil
}
