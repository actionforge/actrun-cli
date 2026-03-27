package agent

// RawCounters holds raw cumulative counters from the OS.
// Deltas are computed between successive snapshots.
type RawCounters struct {
	CPUBusy    uint64
	CPUTotal   uint64
	NetRxBytes uint64
	NetTxBytes uint64
	MemUsedBytes  uint64
	MemTotalBytes uint64

	// CPUInstant is set on platforms (e.g. macOS) where the CPU reading
	// is already a percentage rather than cumulative ticks.
	// When true, CPUBusy is the current usage * 100 and deltas should not be computed.
	CPUInstant bool
}
