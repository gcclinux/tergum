//go:build windows

package webui

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

type MEMORYSTATUSEX struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// getSystemStats returns CPU load percentage, memory used, memory total, and memory percentage.
func getSystemStats() (cpuLoad, memUsed, memTotal, memPercent string) {
	cpuLoad = getCPULoad()
	memUsed, memTotal, memPercent = getMemoryStats()
	return
}

// getCPULoad reads system times twice to calculate CPU usage percentage on Windows.
func getCPULoad() string {
	idle1, kernel1, user1, err := getSystemTimes()
	if err != nil {
		return "N/A"
	}
	time.Sleep(100 * time.Millisecond)
	idle2, kernel2, user2, err := getSystemTimes()
	if err != nil {
		return "N/A"
	}

	idle := idle2 - idle1
	kernel := kernel2 - kernel1
	user := user2 - user1

	system := kernel + user
	if system == 0 {
		return "0%"
	}

	// Kernel time includes idle time, so active is (kernel - idle) + user.
	active := kernel + user - idle
	if active > system {
		active = system
	}

	usage := 100.0 * float64(active) / float64(system)
	return fmt.Sprintf("%.1f%%", usage)
}

// getSystemTimes retrieves the system idle, kernel, and user times.
func getSystemTimes() (idle, kernel, user uint64, err error) {
	var lpIdleTime, lpKernelTime, lpUserTime syscall.Filetime
	r1, _, e1 := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&lpIdleTime)),
		uintptr(unsafe.Pointer(&lpKernelTime)),
		uintptr(unsafe.Pointer(&lpUserTime)),
	)
	if r1 == 0 {
		if e1 != nil && e1.Error() != "The operation completed successfully." {
			err = e1
		} else {
			err = syscall.EINVAL
		}
		return
	}

	idle = (uint64(lpIdleTime.HighDateTime) << 32) | uint64(lpIdleTime.LowDateTime)
	kernel = (uint64(lpKernelTime.HighDateTime) << 32) | uint64(lpKernelTime.LowDateTime)
	user = (uint64(lpUserTime.HighDateTime) << 32) | uint64(lpUserTime.LowDateTime)
	return
}

// getMemoryStats retrieves memory usage info using GlobalMemoryStatusEx.
func getMemoryStats() (used, total, percent string) {
	var memInfo MEMORYSTATUSEX
	memInfo.Length = uint32(unsafe.Sizeof(memInfo))
	r1, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memInfo)))
	if r1 == 0 {
		return "N/A", "N/A", "N/A"
	}

	totalBytes := int64(memInfo.TotalPhys)
	availBytes := int64(memInfo.AvailPhys)
	usedBytes := totalBytes - availBytes
	pct := float64(memInfo.MemoryLoad)

	total = formatSize(totalBytes)
	used = formatSize(usedBytes)
	percent = fmt.Sprintf("%.1f%%", pct)
	return
}
