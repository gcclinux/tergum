//go:build linux

package identity

import (
	"os"
	"strings"
)

func getPlatformMachineID() string {
	// 1. Check DMI product UUID (stable across OS reinstall on same hardware)
	if data, err := os.ReadFile("/sys/class/dmi/id/product_uuid"); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}

	// 2. Fallback to /etc/machine-id
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}

	// 3. Fallback to /var/lib/dbus/machine-id
	if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}

	return ""
}
