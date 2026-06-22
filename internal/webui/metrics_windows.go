//go:build windows

package webui

import (
	"golang.org/x/sys/windows"
)

// diskUsagePercent returns the percentage of disk space used on the volume
// containing the given path. Returns 0 if the path is empty or stats cannot be obtained.
func diskUsagePercent(path string) float64 {
	if path == "" {
		return 0
	}
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(ptr, &freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes)
	if err != nil {
		return 0
	}
	if totalNumberOfBytes == 0 {
		return 0
	}
	usedBytes := totalNumberOfBytes - freeBytesAvailable
	return float64(usedBytes) / float64(totalNumberOfBytes) * 100
}
