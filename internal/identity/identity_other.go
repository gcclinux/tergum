//go:build !linux && !windows && !darwin

package identity

func getPlatformMachineID() string {
	return ""
}
