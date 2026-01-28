//go:build !darwin && !linux

package cli

import "fmt"

func installLaunchdService(concurrency int) error {
	return fmt.Errorf("launchd not available on this platform")
}

func uninstallLaunchdService() error {
	return fmt.Errorf("launchd not available on this platform")
}

func installSystemdService(concurrency int) error {
	return fmt.Errorf("systemd not available on this platform")
}

func uninstallSystemdService() error {
	return fmt.Errorf("systemd not available on this platform")
}
