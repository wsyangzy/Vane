//go:build windows

package monitor

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

type processMemoryCounters struct {
	Size, PageFaults                                             uint32
	PeakWorkingSet, WorkingSet                                   uintptr
	QuotaPeakPaged, QuotaPaged, QuotaPeakNonPaged, QuotaNonPaged uintptr
	Pagefile, PeakPagefile, Private                              uintptr
}

func CurrentProcessMemory() (ProcessMemoryStats, error) {
	var counters processMemoryCounters
	counters.Size = uint32(unsafe.Sizeof(counters))
	ok, _, err := getProcessMemoryInfo.Call(^uintptr(0), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	if ok == 0 {
		return ProcessMemoryStats{}, fmt.Errorf("read process memory: %w", err)
	}
	return ProcessMemoryStats{WorkingSetBytes: uint64(counters.WorkingSet), PrivateBytes: uint64(counters.Private), PrivateAvailable: true}, nil
}
