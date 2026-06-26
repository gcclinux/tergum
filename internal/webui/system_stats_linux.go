//go:build linux

package webui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// getSystemStats returns CPU load percentage, memory used, memory total, and memory percentage.
func getSystemStats() (cpuLoad, memUsed, memTotal, memPercent string) {
	cpuLoad = getCPULoad()
	memUsed, memTotal, memPercent = getMemoryStats()
	return
}

// getCPULoad reads /proc/stat twice with a short interval to calculate CPU usage percentage.
func getCPULoad() string {
	idle1, total1 := readCPUStat()
	if total1 == 0 {
		return "N/A"
	}
	time.Sleep(100 * time.Millisecond)
	idle2, total2 := readCPUStat()
	if total2 == 0 {
		return "N/A"
	}

	idleDelta := idle2 - idle1
	totalDelta := total2 - total1
	if totalDelta == 0 {
		return "0%"
	}

	usage := 100.0 * float64(totalDelta-idleDelta) / float64(totalDelta)
	return fmt.Sprintf("%.1f%%", usage)
}

// readCPUStat reads the first "cpu" line from /proc/stat and returns idle and total jiffies.
func readCPUStat() (idle, total uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				var sum uint64
				for i := 1; i < len(fields); i++ {
					val, err := strconv.ParseUint(fields[i], 10, 64)
					if err != nil {
						continue
					}
					sum += val
					if i == 4 { // idle is the 4th value (index 4, field[4])
						idle = val
					}
				}
				total = sum
			}
		}
	}
	return
}

// getMemoryStats reads /proc/meminfo and returns used, total, and percentage.
func getMemoryStats() (used, total, percent string) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return "N/A", "N/A", "N/A"
	}
	defer f.Close()

	var memTotal, memAvailable uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			memTotal = parseMemInfoValue(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			memAvailable = parseMemInfoValue(line)
		}
		if memTotal > 0 && memAvailable > 0 {
			break
		}
	}

	if memTotal == 0 {
		return "N/A", "N/A", "N/A"
	}

	memUsed := memTotal - memAvailable
	pct := 100.0 * float64(memUsed) / float64(memTotal)

	// Convert from kB to bytes for formatSize.
	total = formatSize(int64(memTotal * 1024))
	used = formatSize(int64(memUsed * 1024))
	percent = fmt.Sprintf("%.1f%%", pct)
	return
}

// parseMemInfoValue extracts the numeric kB value from a /proc/meminfo line.
func parseMemInfoValue(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			return val
		}
	}
	return 0
}
