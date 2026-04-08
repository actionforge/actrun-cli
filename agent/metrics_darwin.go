//go:build darwin

package agent

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Snapshot returns current cumulative CPU and network counters on macOS.
func Snapshot() (RawCounters, error) {
	var c RawCounters

	// CPU from top -l 1
	cpuOut, err := exec.Command("top", "-l", "1", "-s", "0", "-n", "0").Output()
	if err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(cpuOut))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "CPU usage:") {
				// "CPU usage: 5.26% user, 10.52% sys, 84.21% idle"
				parts := strings.Fields(line)
				for i, p := range parts {
					if p == "idle" && i > 0 {
						idleStr := strings.TrimSuffix(parts[i-1], "%")
						idle, e := strconv.ParseFloat(idleStr, 64)
						if e == nil {
							c.CPUTotal = 10000
							c.CPUBusy = uint64((100.0 - idle) * 100)
							c.CPUInstant = true
						}
					}
				}
				break
			}
		}
	}

	// Network from netstat -ibn
	netOut, err := exec.Command("netstat", "-ibn").Output()
	if err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(netOut))
		first := true
		var ibyteIdx, obyteIdx int
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if first {
				first = false
				for i, f := range fields {
					switch f {
					case "Ibytes":
						ibyteIdx = i
					case "Obytes":
						obyteIdx = i
					}
				}
				continue
			}
			if len(fields) <= ibyteIdx || len(fields) <= obyteIdx {
				continue
			}
			// Skip loopback
			if strings.HasPrefix(fields[0], "lo") {
				continue
			}
			// Only physical interfaces (en*, eth*)
			name := fields[0]
			if !strings.HasPrefix(name, "en") && !strings.HasPrefix(name, "eth") {
				continue
			}
			rx, e1 := strconv.ParseUint(fields[ibyteIdx], 10, 64)
			tx, e2 := strconv.ParseUint(fields[obyteIdx], 10, 64)
			if e1 != nil || e2 != nil {
				continue
			}
			c.NetRxBytes += rx
			c.NetTxBytes += tx
		}
	}

	// Memory from sysctl + vm_stat
	memOut, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err == nil {
		totalStr := strings.TrimSpace(string(memOut))
		if total, e := strconv.ParseUint(totalStr, 10, 64); e == nil {
			c.MemTotalBytes = total
		}
	}
	vmOut, err := exec.Command("vm_stat").Output()
	if err == nil {
		var pageSize uint64 = 4096 // default macOS page size
		var free, inactive, speculative uint64
		vmScanner := bufio.NewScanner(bytes.NewReader(vmOut))
		for vmScanner.Scan() {
			line := vmScanner.Text()
			if strings.HasPrefix(line, "Mach Virtual Memory Statistics") {
				// "...page size of NNNN bytes)"
				if idx := strings.Index(line, "page size of "); idx != -1 {
					sub := line[idx+len("page size of "):]
					sub = strings.TrimSuffix(sub, " bytes)")
					if v, e := strconv.ParseUint(sub, 10, 64); e == nil {
						pageSize = v
					}
				}
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			valStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[1]), "."))
			val, e := strconv.ParseUint(valStr, 10, 64)
			if e != nil {
				continue
			}
			switch key {
			case "Pages free":
				free = val
			case "Pages inactive":
				inactive = val
			case "Pages speculative":
				speculative = val
			}
		}
		if c.MemTotalBytes > 0 {
			freeBytes := (free + inactive + speculative) * pageSize
			if freeBytes > c.MemTotalBytes {
				freeBytes = c.MemTotalBytes
			}
			c.MemUsedBytes = c.MemTotalBytes - freeBytes
		}
	}

	if c.CPUTotal == 0 {
		return c, fmt.Errorf("failed to read CPU stats")
	}
	return c, nil
}
