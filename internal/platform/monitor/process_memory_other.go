//go:build !windows

package monitor

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

func CurrentProcessMemory() (ProcessMemoryStats, error) {
	if runtime.GOOS != "linux" {
		return ProcessMemoryStats{}, fmt.Errorf("process memory counters unavailable on %s", runtime.GOOS)
	}
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return ProcessMemoryStats{}, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return ProcessMemoryStats{}, fmt.Errorf("invalid process memory counters")
	}
	resident, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return ProcessMemoryStats{}, err
	}
	return ProcessMemoryStats{WorkingSetBytes: resident * uint64(os.Getpagesize())}, nil
}
