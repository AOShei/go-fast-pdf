//go:build linux

package pdftest

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

// CountInstructions starts counting the instructions that the calling
// thread executes in user space. read returns the count so far and
// stop ends the counting. ok is false where the system does not allow
// it, and then both do nothing.
//
// The count does not depend on what else the machine is doing, which
// the time a benchmark takes does. The caller keeps its goroutine on
// one thread (runtime.LockOSThread) for as long as it counts; work done
// on other threads, such as most of the garbage collector's, is not
// counted.
func CountInstructions() (read func() uint64, stop func(), ok bool) {
	// struct perf_event_attr, at the size of its fifth version.
	var attr [112]byte
	binary.NativeEndian.PutUint32(attr[0:], 0)   // type: a hardware event
	binary.NativeEndian.PutUint32(attr[4:], 112) // size
	binary.NativeEndian.PutUint64(attr[8:], 1)   // config: instructions
	// exclude_kernel and exclude_hv: counting them needs a privilege.
	binary.NativeEndian.PutUint64(attr[40:], 1<<5|1<<6)
	fd, _, errno := syscall.Syscall6(syscall.SYS_PERF_EVENT_OPEN, uintptr(unsafe.Pointer(&attr[0])), 0, ^uintptr(0), ^uintptr(0), 0, 0)
	if errno != 0 {
		return func() uint64 { return 0 }, func() {}, false
	}
	read = func() uint64 {
		var buf [8]byte
		if n, err := syscall.Read(int(fd), buf[:]); n != 8 || err != nil {
			return 0
		}
		return binary.NativeEndian.Uint64(buf[:])
	}
	return read, func() { syscall.Close(int(fd)) }, true
}
