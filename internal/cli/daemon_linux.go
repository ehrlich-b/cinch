//go:build linux

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"text/template"
)

// Stubs for macOS-only functions
func installLaunchdService(concurrency int) error {
	return fmt.Errorf("launchd not available on Linux")
}

func uninstallLaunchdService() error {
	return fmt.Errorf("launchd not available on Linux")
}

const systemdServiceTemplate = `[Unit]
Description=Cinch CI Worker Daemon
After=network.target

[Service]
Type=simple
ExecStart={{.Executable}} daemon run -n {{.Concurrency}}
Restart=always
RestartSec=5

# Logging
StandardOutput=append:{{.LogFile}}
StandardError=append:{{.LogFile}}

# Security
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths={{.ConfigDir}}

[Install]
WantedBy=default.target
`

func systemdServicePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", "cinch-daemon.service")
}

func installSystemdService(concurrency int) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}

	servicePath := systemdServicePath()

	// Ensure systemd user directory exists
	if err := os.MkdirAll(filepath.Dir(servicePath), 0755); err != nil {
		return fmt.Errorf("create systemd directory: %w", err)
	}

	// Generate service file
	tmpl, err := template.New("service").Parse(systemdServiceTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	f, err := os.Create(servicePath)
	if err != nil {
		return fmt.Errorf("create service file: %w", err)
	}
	defer f.Close()

	data := struct {
		Executable  string
		Concurrency string
		LogFile     string
		ConfigDir   string
	}{
		Executable:  executable,
		Concurrency: strconv.Itoa(concurrency),
		LogFile:     filepath.Join(home, ".cinch", "daemon.log"),
		ConfigDir:   filepath.Join(home, ".cinch"),
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("write service: %w", err)
	}

	// Reload systemd
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	if err := cmd.Run(); err != nil {
		fmt.Println("Note: Failed to reload systemd. You may need to run:")
		fmt.Println("  systemctl --user daemon-reload")
	}

	fmt.Printf("Service installed at %s\n", servicePath)
	fmt.Println()
	fmt.Println("To start the daemon:")
	fmt.Println("  cinch daemon start")
	fmt.Println()
	fmt.Println("Or use systemctl directly:")
	fmt.Println("  systemctl --user enable cinch-daemon")
	fmt.Println("  systemctl --user start cinch-daemon")

	return nil
}

func uninstallSystemdService() error {
	servicePath := systemdServicePath()

	// Try to stop and disable first
	_ = exec.Command("systemctl", "--user", "stop", "cinch-daemon").Run()
	_ = exec.Command("systemctl", "--user", "disable", "cinch-daemon").Run()

	if err := os.Remove(servicePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service not installed")
		}
		return fmt.Errorf("remove service file: %w", err)
	}

	// Reload systemd
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	fmt.Println("Service uninstalled")
	return nil
}
