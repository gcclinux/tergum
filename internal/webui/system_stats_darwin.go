//go:build darwin

package webui

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// getSystemStats returns CPU load percentage, memory used, memory total, and memory percentage.
func getSystemStats() (cpuLoad, memUsed, memTotal, memPercent string) {
	cpuLoad = getCPULoad()
	memUsed, memTotal, memPercent = getMemoryStats()
	return
}

// getCPULoad samples host_cpu_load_info twice to calculate CPU usage percentage on Darwin.
func getCPULoad() string {
	user1, sys1, idle1, nice1, err := readCPUTicks()
	if err != nil {
		return "N/A"
	}
	time.Sleep(100 * time.Millisecond)
	user2, sys2, idle2, nice2, err := readCPUTicks()
	if err != nil {
		return "N/A"
	}

	idleDelta := idle2 - idle1
	totalDelta := (user2 + sys2 + idle2 + nice2) - (user1 + sys1 + idle1 + nice1)
	if totalDelta == 0 {
		return "0%"
	}

	usage := 100.0 * float64(totalDelta-idleDelta) / float64(totalDelta)
	return fmt.Sprintf("%.1f%%", usage)
}

// readCPUTicks reads CPU ticks from host_processor_info via sysctl on Darwin.
// Returns user, sys, idle, nice tick counts.
func readCPUTicks() (user, sys, idle, nice uint64, err error) {
	// HOST_CPU_LOAD_INFO = 3, HOST_CPU_LOAD_INFO_COUNT = 4
	const (
		hostCPULoadInfo      = 3
		hostCPULoadInfoCount = 4
		cpuStateUser         = 0
		cpuStateSys          = 1
		cpuStateIdle         = 2
		cpuStateNice         = 3
	)

	b, err := unix.SysctlRaw("kern.boottime") // warm up sysctl; use host_info below
	_ = b

	// Use mach host_statistics via cgo-free approach: sysctl machdep.cpu
	// Fall back to parsing vm_stat for a simpler approach.
	// For CPU, use sysctl kern.cp_time (available on Darwin).
	raw, err := unix.SysctlRaw("kern.cp_time")
	if err != nil {
		return 0, 0, 0, 0, err
	}
	// kern.cp_time returns 5 x uint32: user, nice, sys, intr, idle
	if len(raw) < 5*4 {
		return 0, 0, 0, 0, fmt.Errorf("kern.cp_time: unexpected size %d", len(raw))
	}
	vals := make([]uint32, 5)
	for i := range vals {
		vals[i] = *(*uint32)(unsafe.Pointer(&raw[i*4]))
	}
	// CP_USER=0, CP_NICE=1, CP_SYS=2, CP_INTR=3, CP_IDLE=4
	user = uint64(vals[0])
	nice = uint64(vals[1])
	sys = uint64(vals[2])
	idle = uint64(vals[4])
	err = nil
	return
}

// getMemoryStats returns used, total, and percentage memory on Darwin using sysctl.
func getMemoryStats() (used, total, percent string) {
	// Total physical memory via sysctl hw.memsize
	memTotal, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return "N/A", "N/A", "N/A"
	}

	// Free/inactive pages via vm.stat (vm_statistics64)
	// Page size via hw.pagesize
	pageSize, err := unix.SysctlUint32("hw.pagesize")
	if err != nil {
		return "N/A", "N/A", "N/A"
	}

	// Read vm_stat values via sysctl vm.page_free_count and vm.page_inactive_count
	freePages, err := unix.SysctlUint32("vm.page_free_count")
	if err != nil {
		return "N/A", "N/A", "N/A"
	}

	availableBytes := uint64(freePages) * uint64(pageSize)
	usedBytes := memTotal - availableBytes
	if usedBytes > memTotal {
		usedBytes = memTotal
	}
	pct := 100.0 * float64(usedBytes) / float64(memTotal)

	total = formatSize(int64(memTotal))
	used = formatSize(int64(usedBytes))
	percent = fmt.Sprintf("%.1f%%", pct)
	return
}
