// Package identity provides system and machine identity detection for Tergum.
// It gathers stable machine fingerprints, hostname, and OS family to uniquely
// identify client nodes across OS rebuilds and prevent cross-OS database corruption.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"runtime"
	"sort"
	"strings"
)

// SystemIdentity holds the unique identification and environment details of a node.
type SystemIdentity struct {
	Hostname  string   `json:"hostname"`
	OSFamily  string   `json:"os_family"` // "linux", "windows", "darwin", etc.
	MachineID string   `json:"machine_id"` // stable hardware/system identifier
	LocalIPs  []string `json:"local_ips"`
}

// GetSystemIdentity inspects the current host and returns a SystemIdentity.
func GetSystemIdentity() SystemIdentity {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	macs := getHardwareMACs()
	rawID := getPlatformMachineID()

	// If no platform-specific ID was found, combine hostname and MAC addresses.
	if rawID == "" {
		rawID = hostname + ":" + strings.Join(macs, ",")
	} else if len(macs) > 0 {
		rawID = rawID + ":" + macs[0]
	}

	h := sha256.Sum256([]byte(rawID))
	machineID := hex.EncodeToString(h[:16]) // 32 hex chars

	return SystemIdentity{
		Hostname:  hostname,
		OSFamily:  runtime.GOOS,
		MachineID: machineID,
		LocalIPs:  getLocalIPv4s(),
	}
}

// CheckOSCompatibility verifies whether a target client's backed-up OS family
// is compatible with this host's OS family.
// Returns nil if compatible, or an error describing the incompatibility.
func CheckOSCompatibility(storedOS, currentOS string) error {
	storedOS = strings.TrimSpace(strings.ToLower(storedOS))
	currentOS = strings.TrimSpace(strings.ToLower(currentOS))

	if storedOS == "" || currentOS == "" {
		return nil // cannot determine, allow with caution
	}

	if storedOS == currentOS {
		return nil
	}

	// Cross-OS is incompatible (e.g. Windows paths C:\... vs Unix /...)
	return &IncompatibleOSError{
		StoredOS:  storedOS,
		CurrentOS: currentOS,
	}
}

// IncompatibleOSError is returned when a client database belongs to a different OS family.
type IncompatibleOSError struct {
	StoredOS  string
	CurrentOS string
}

func (e *IncompatibleOSError) Error() string {
	return "incompatible OS: client database was created on " + e.StoredOS +
		", but current system is " + e.CurrentOS +
		" (cross-OS database recovery is not supported)"
}

// getHardwareMACs returns sorted MAC addresses of physical network interfaces.
func getHardwareMACs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var macs []string
	for _, iface := range ifaces {
		// Ignore loopback, down, or empty hardware address
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 || len(iface.HardwareAddr) == 0 {
			continue
		}
		macStr := strings.ToLower(iface.HardwareAddr.String())
		if macStr != "" && !isVirtualMAC(macStr) {
			macs = append(macs, macStr)
		}
	}
	sort.Strings(macs)
	return macs
}

// isVirtualMAC checks for common virtual interface prefixes.
func isVirtualMAC(mac string) bool {
	prefixes := []string{"00:05:69", "00:0c:29", "00:50:56", "00:1c:42", "08:00:27"}
	for _, p := range prefixes {
		if strings.HasPrefix(mac, p) {
			return true
		}
	}
	return false
}

// getLocalIPv4s returns non-loopback IPv4 addresses.
func getLocalIPv4s() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []string
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				ips = append(ips, ip4.String())
			}
		}
	}
	sort.Strings(ips)
	return ips
}
