//go:build windows

package monitor

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestProcessMemoryIncludesNativeAllocationsOutsideGoHeap(t *testing.T) {
	runtime.GC()
	before, err := CurrentProcessMemory()
	if err != nil {
		t.Fatal(err)
	}
	var goBefore, goAfter runtime.MemStats
	runtime.ReadMemStats(&goBefore)
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	const size = 16 << 20
	address, _, allocErr := kernel.NewProc("VirtualAlloc").Call(0, size, 0x3000, 0x04)
	if address == 0 {
		t.Fatal(allocErr)
	}
	t.Cleanup(func() {
		ok, _, err := kernel.NewProc("VirtualFree").Call(address, 0, 0x8000)
		if ok == 0 {
			t.Error(err)
		}
	})
	buffer := unsafe.Slice((*byte)(unsafe.Pointer(address)), size)
	for i := 0; i < len(buffer); i += 4096 {
		buffer[i] = 1
	}
	after, err := CurrentProcessMemory()
	if err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&goAfter)
	if !after.PrivateAvailable || after.PrivateBytes < before.PrivateBytes+size/2 || after.WorkingSetBytes < before.WorkingSetBytes+size/2 {
		t.Fatalf("native allocation missing: before=%+v after=%+v", before, after)
	}
	if goAfter.Alloc > goBefore.Alloc+size/4 {
		t.Fatal("fixture allocated its payload on the Go heap")
	}
}
