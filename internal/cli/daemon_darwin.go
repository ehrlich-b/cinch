//go:build darwin

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"text/template"
)

// Stubs for Linux-only functions
func installSystemdService(concurrency int) error {
	return fmt.Errorf("systemd not available on macOS")
}

func uninstallSystemdService() error {
	return fmt.Errorf("systemd not available on macOS")
}

const launchdPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>sh.cinch.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Executable}}</string>
        <string>daemon</string>
        <string>run</string>
        <string>-n</string>
        <string>{{.Concurrency}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogFile}}</string>
    <key>StandardErrorPath</key>
    <string>{{.LogFile}}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin</string>
    </dict>
</dict>
</plist>
`

func launchdPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "sh.cinch.daemon.plist")
}

func installLaunchdService(concurrency int) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}

	plistPath := launchdPlistPath()

	// Ensure LaunchAgents directory exists
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}

	// Generate plist file
	tmpl, err := template.New("plist").Parse(launchdPlistTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	f, err := os.Create(plistPath)
	if err != nil {
		return fmt.Errorf("create plist file: %w", err)
	}
	defer f.Close()

	data := struct {
		Executable  string
		Concurrency string
		LogFile     string
	}{
		Executable:  executable,
		Concurrency: strconv.Itoa(concurrency),
		LogFile:     filepath.Join(home, ".cinch", "daemon.log"),
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	fmt.Printf("Service installed at %s\n", plistPath)
	fmt.Println()
	fmt.Println("To start the daemon:")
	fmt.Println("  cinch daemon start")
	fmt.Println()
	fmt.Println("Or load immediately with launchctl:")
	fmt.Printf("  launchctl load %s\n", plistPath)

	return nil
}

func uninstallLaunchdService() error {
	plistPath := launchdPlistPath()

	// Try to unload first
	cmd := exec.Command("launchctl", "unload", plistPath)
	_ = cmd.Run() // Ignore errors if not loaded

	if err := os.Remove(plistPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service not installed")
		}
		return fmt.Errorf("remove plist: %w", err)
	}

	fmt.Println("Service uninstalled")
	return nil
}
