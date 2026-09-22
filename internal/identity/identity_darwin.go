//go:build darwin

package identity

import (
	"os/exec"
	"strings"
)

func getPlatformMachineID() string {
	// Query IOPlatformUUID using ioreg
	cmd := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.Split(line, "=")
			if len(parts) >= 2 {
				uuid := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				return uuid
			}
		}
	}
	return ""
}
