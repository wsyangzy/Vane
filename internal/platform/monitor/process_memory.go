package monitor

// ProcessMemoryStats includes allocations outside the Go heap. WorkingSet is
// resident memory; PrivateBytes is committed private memory where available.
type ProcessMemoryStats struct {
	WorkingSetBytes  uint64
	PrivateBytes     uint64
	PrivateAvailable bool
}
