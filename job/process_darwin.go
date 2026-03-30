package job

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"
)

// getProcessStartTime returns the process start time on macOS using sysctl.
// The returned value is the kernel boot-relative start time in nanoseconds.
func getProcessStartTime(pid int) (int64, error) {
	const (
		ctlKern        = 1
		kernProc       = 14
		kernProcPID    = 1
		kinfoSize      = 648 // sizeof(struct kinfo_proc) on darwin arm64/amd64
	)
	mib := [4]int32{ctlKern, kernProc, kernProcPID, int32(pid)}
	var buf [kinfoSize]byte
	n := uintptr(len(buf))

	_, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])),
		4,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)),
		0, 0,
	)
	if errno != 0 {
		return 0, fmt.Errorf("sysctl: %v", errno)
	}

	// kp_proc.p_starttime is a struct timeval at offset 128 in kinfo_proc (on darwin).
	// timeval: tv_sec (8 bytes) + tv_usec (8 bytes) on 64-bit.
	const startTimeOffset = 128
	if n < startTimeOffset+16 {
		return 0, fmt.Errorf("sysctl buffer too small: %d", n)
	}
	tvSec := int64(binary.LittleEndian.Uint64(buf[startTimeOffset:]))
	tvUsec := int64(binary.LittleEndian.Uint64(buf[startTimeOffset+8:]))

	return tvSec*1e9 + tvUsec*1e3, nil
}
