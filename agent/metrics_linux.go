//go:build linux

package agent

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Snapshot returns current cumulative CPU and network counters on Linux.
func Snapshot() (RawCounters, error) {
	var c RawCounters

	// CPU from /proc/stat
	statFile, err := os.Open("/proc/stat")
	if err != nil {
		return c, fmt.Errorf("open /proc/stat: %w", err)
	}
	defer statFile.Close()

	scanner := bufio.NewScanner(statFile)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			break
		}
		// fields: cpu user nice system idle ...
		var total uint64
		for _, f := range fields[1:] {
			v, _ := strconv.ParseUint(f, 10, 64)
			total += v
		}
		idle, _ := strconv.ParseUint(fields[4], 10, 64)
		c.CPUTotal = total
		c.CPUBusy = total - idle
		break
	}

	// Network from /proc/net/dev
	devFile, err := os.Open("/proc/net/dev")
	if err != nil {
		return c, fmt.Errorf("open /proc/net/dev: %w", err)
	}
	defer devFile.Close()

	scanner = bufio.NewScanner(devFile)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 10 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		c.NetRxBytes += rx
		c.NetTxBytes += tx
	}

	// Memory from /proc/meminfo
	memFile, err := os.Open("/proc/meminfo")
	if err == nil {
		defer memFile.Close()
		scanner = bufio.NewScanner(memFile)
		var memTotal, memAvailable uint64
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					v, _ := strconv.ParseUint(fields[1], 10, 64)
					memTotal = v * 1024 // kB to bytes
				}
			} else if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					v, _ := strconv.ParseUint(fields[1], 10, 64)
					memAvailable = v * 1024
				}
			}
		}
		if memTotal > 0 {
			c.MemTotalBytes = memTotal
			c.MemUsedBytes = memTotal - memAvailable
		}
	}

	if c.CPUTotal == 0 {
		return c, fmt.Errorf("failed to read CPU stats")
	}
	return c, nil
}
