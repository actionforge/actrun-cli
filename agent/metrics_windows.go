//go:build windows

package agent

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")
	modiphlpapi = syscall.NewLazyDLL("iphlpapi.dll")

	procGetSystemTimes       = modkernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
	procGetIfTable           = modiphlpapi.NewProc("GetIfTable")
)

type fileTime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

func (ft fileTime) toUint64() uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

type memoryStatusEx struct {
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

type mibIfRow struct {
	Name            [256]uint16
	Index           uint32
	Type            uint32
	Mtu             uint32
	Speed           uint32
	PhysAddrLen     uint32
	PhysAddr        [8]byte
	AdminStatus     uint32
	OperStatus      uint32
	LastChange      uint32
	InOctets        uint32
	InUcastPkts     uint32
	InNUcastPkts    uint32
	InDiscards      uint32
	InErrors        uint32
	InUnknownProtos uint32
	OutOctets       uint32
	OutUcastPkts    uint32
	OutNUcastPkts   uint32
	OutDiscards     uint32
	OutErrors       uint32
	OutQLen         uint32
	DescrLen        uint32
	Descr           [256]byte
}

type mibIfTable struct {
	NumEntries uint32
	Table      [1]mibIfRow
}

// Snapshot returns current cumulative CPU and network counters on Windows.
func Snapshot() (RawCounters, error) {
	var c RawCounters

	// CPU from GetSystemTimes
	var idleTime, kernelTime, userTime fileTime
	ret, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if ret == 0 {
		return c, fmt.Errorf("GetSystemTimes: %w", err)
	}
	idle := idleTime.toUint64()
	kernel := kernelTime.toUint64()
	user := userTime.toUint64()
	// kernel time includes idle time on Windows
	c.CPUTotal = kernel + user
	c.CPUBusy = c.CPUTotal - idle

	// Memory from GlobalMemoryStatusEx
	var memStatus memoryStatusEx
	memStatus.Length = uint32(unsafe.Sizeof(memStatus))
	ret, _, _ = procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memStatus)))
	if ret != 0 {
		c.MemTotalBytes = memStatus.TotalPhys
		c.MemUsedBytes = memStatus.TotalPhys - memStatus.AvailPhys
	}

	// Network from GetIfTable
	var size uint32
	// First call to get required buffer size
	procGetIfTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size > 0 {
		buf := make([]byte, size)
		ret, _, _ = procGetIfTable.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			0,
		)
		if ret == 0 {
			table := (*mibIfTable)(unsafe.Pointer(&buf[0]))
			rows := unsafe.Slice(&table.Table[0], table.NumEntries)
			for _, row := range rows {
				// Skip loopback (24) and tunnel (131) interfaces
				if row.Type == 24 || row.Type == 131 {
					continue
				}
				c.NetRxBytes += uint64(row.InOctets)
				c.NetTxBytes += uint64(row.OutOctets)
			}
		}
	}

	if c.CPUTotal == 0 {
		return c, fmt.Errorf("failed to read CPU stats")
	}
	return c, nil
}
