package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
)

const serverSystemdTemplate = `[Unit]
Description=Cinch CI Server
After=network.target

[Service]
Type=simple
User={{.User}}
Group={{.Group}}
WorkingDirectory={{.DataDir}}
EnvironmentFile={{.EnvFile}}
ExecStart={{.Executable}} server
Restart=always
RestartSec=5

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths={{.DataDir}}

[Install]
WantedBy=multi-user.target
`

const serverLaunchdTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>sh.cinch.server</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Executable}}</string>
        <string>server</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>{{.DataDir}}</string>
    <key>StandardOutPath</key>
    <string>{{.LogFile}}</string>
    <key>StandardErrorPath</key>
    <string>{{.LogFile}}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin</string>
{{.EnvVars}}
    </dict>
</dict>
</plist>
`

// InstallServer installs cinch server as a system service
func InstallServer(relay bool) error {
	// Check for required env vars
	secretKey := os.Getenv("CINCH_SECRET_KEY")
	if secretKey == "" {
		return fmt.Errorf("CINCH_SECRET_KEY is required. Generate with: openssl rand -hex 32")
	}

	// Collect all CINCH_* env vars
	envVars := collectCinchEnvVars()
	if relay {
		envVars["CINCH_RELAY"] = "true"
	}

	switch runtime.GOOS {
	case "linux":
		return installServerSystemd(envVars)
	case "darwin":
		return installServerLaunchd(envVars)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func collectCinchEnvVars() map[string]string {
	vars := make(map[string]string)
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "CINCH_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				vars[parts[0]] = parts[1]
			}
		}
	}
	return vars
}

func installServerSystemd(envVars map[string]string) error {
	// Check if running as root
	if os.Geteuid() != 0 {
		return fmt.Errorf("server install requires root. Run with sudo")
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Resolve to absolute path (important for systemd)
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	// Create directories
	envDir := "/etc/cinch"
	dataDir := envVars["CINCH_DATA_DIR"]
	if dataDir == "" {
		dataDir = "/var/lib/cinch"
	}

	if err := os.MkdirAll(envDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", envDir, err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", dataDir, err)
	}

	// Write environment file
	envFile := filepath.Join(envDir, "env")
	f, err := os.OpenFile(envFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create env file: %w", err)
	}
	for k, v := range envVars {
		fmt.Fprintf(f, "%s=%s\n", k, v)
	}
	// Ensure data dir is set
	if envVars["CINCH_DATA_DIR"] == "" {
		fmt.Fprintf(f, "CINCH_DATA_DIR=%s\n", dataDir)
	}
	f.Close()

	// Determine user/group (prefer 'cinch' if exists, otherwise current user's sudo caller)
	user := "root"
	group := "root"
	if _, err := exec.Command("id", "cinch").Output(); err == nil {
		user = "cinch"
		group = "cinch"
		// Change ownership of data dir and env file
		_ = exec.Command("chown", "-R", "cinch:cinch", dataDir).Run()
		_ = exec.Command("chown", "cinch:cinch", envFile).Run()
	} else if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		user = sudoUser
		group = sudoUser
		_ = exec.Command("chown", "-R", user+":"+group, dataDir).Run()
		_ = exec.Command("chown", user+":"+group, envFile).Run()
	}

	// Generate service file
	servicePath := "/etc/systemd/system/cinch-server.service"
	tmpl, err := template.New("service").Parse(serverSystemdTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	sf, err := os.Create(servicePath)
	if err != nil {
		return fmt.Errorf("create service file: %w", err)
	}
	defer sf.Close()

	data := struct {
		Executable string
		User       string
		Group      string
		DataDir    string
		EnvFile    string
	}{
		Executable: executable,
		User:       user,
		Group:      group,
		DataDir:    dataDir,
		EnvFile:    envFile,
	}

	if err := tmpl.Execute(sf, data); err != nil {
		return fmt.Errorf("write service: %w", err)
	}

	// Reload systemd
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		fmt.Println("Warning: failed to reload systemd")
	}

	fmt.Printf("Service installed: %s\n", servicePath)
	fmt.Printf("Environment file: %s\n", envFile)
	fmt.Printf("Data directory: %s\n", dataDir)
	fmt.Printf("Running as user: %s\n", user)
	fmt.Println()
	fmt.Println("To start:")
	fmt.Println("  sudo systemctl enable cinch-server")
	fmt.Println("  sudo systemctl start cinch-server")
	fmt.Println()
	fmt.Println("To view logs:")
	fmt.Println("  journalctl -u cinch-server -f")

	return nil
}

func installServerLaunchd(envVars map[string]string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}

	dataDir := envVars["CINCH_DATA_DIR"]
	if dataDir == "" {
		dataDir = filepath.Join(home, ".cinch", "server-data")
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// Build env vars XML
	var envXML strings.Builder
	for k, v := range envVars {
		envXML.WriteString(fmt.Sprintf("        <key>%s</key>\n        <string>%s</string>\n", k, v))
	}
	if envVars["CINCH_DATA_DIR"] == "" {
		envXML.WriteString(fmt.Sprintf("        <key>CINCH_DATA_DIR</key>\n        <string>%s</string>\n", dataDir))
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", "sh.cinch.server.plist")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}

	tmpl, err := template.New("plist").Parse(serverLaunchdTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	f, err := os.Create(plistPath)
	if err != nil {
		return fmt.Errorf("create plist file: %w", err)
	}
	defer f.Close()

	data := struct {
		Executable string
		DataDir    string
		LogFile    string
		EnvVars    string
	}{
		Executable: executable,
		DataDir:    dataDir,
		LogFile:    filepath.Join(home, ".cinch", "server.log"),
		EnvVars:    envXML.String(),
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	fmt.Printf("Service installed: %s\n", plistPath)
	fmt.Printf("Data directory: %s\n", dataDir)
	fmt.Println()
	fmt.Println("To start:")
	fmt.Printf("  launchctl load %s\n", plistPath)
	fmt.Println()
	fmt.Println("To view logs:")
	fmt.Printf("  tail -f %s\n", filepath.Join(home, ".cinch", "server.log"))

	return nil
}

// UninstallServer removes the cinch server system service
func UninstallServer() error {
	switch runtime.GOOS {
	case "linux":
		return uninstallServerSystemd()
	case "darwin":
		return uninstallServerLaunchd()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func uninstallServerSystemd() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("server uninstall requires root. Run with sudo")
	}

	servicePath := "/etc/systemd/system/cinch-server.service"

	// Stop and disable
	_ = exec.Command("systemctl", "stop", "cinch-server").Run()
	_ = exec.Command("systemctl", "disable", "cinch-server").Run()

	if err := os.Remove(servicePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service not installed")
		}
		return fmt.Errorf("remove service file: %w", err)
	}

	_ = exec.Command("systemctl", "daemon-reload").Run()

	fmt.Println("Service uninstalled")
	fmt.Println()
	fmt.Println("Note: /etc/cinch/env and data directory were NOT removed.")
	fmt.Println("Remove manually if no longer needed.")

	return nil
}

func uninstallServerLaunchd() error {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "sh.cinch.server.plist")

	_ = exec.Command("launchctl", "unload", plistPath).Run()

	if err := os.Remove(plistPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service not installed")
		}
		return fmt.Errorf("remove plist: %w", err)
	}

	fmt.Println("Service uninstalled")
	return nil
}

// PromptServerEnvVars interactively prompts for required env vars if not set
func PromptServerEnvVars() error {
	reader := bufio.NewReader(os.Stdin)

	if os.Getenv("CINCH_SECRET_KEY") == "" {
		fmt.Println("CINCH_SECRET_KEY is required.")
		fmt.Print("Generate one now? [Y/n]: ")
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "" || answer == "y" || answer == "yes" {
			out, err := exec.Command("openssl", "rand", "-hex", "32").Output()
			if err != nil {
				return fmt.Errorf("failed to generate secret: %w", err)
			}
			key := strings.TrimSpace(string(out))
			os.Setenv("CINCH_SECRET_KEY", key)
			fmt.Printf("Generated: %s\n", key)
			fmt.Println("SAVE THIS KEY - you need it for recovery/migration")
			fmt.Println()
		} else {
			return fmt.Errorf("CINCH_SECRET_KEY is required")
		}
	}

	// Check for at least one forge token
	hasForgeToken := os.Getenv("CINCH_GITHUB_TOKEN") != "" ||
		os.Getenv("CINCH_GITLAB_TOKEN") != "" ||
		os.Getenv("CINCH_FORGEJO_TOKEN") != ""

	if !hasForgeToken {
		fmt.Println("No forge token found (CINCH_GITHUB_TOKEN, CINCH_GITLAB_TOKEN, or CINCH_FORGEJO_TOKEN)")
		fmt.Println("You'll need to set one before adding repos.")
	}

	return nil
}
