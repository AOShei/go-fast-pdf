//go:build unix

package gofastpdf_test

import (
	"runtime"
	"syscall"
	"time"
)

// cpuTime returns the processor time this process has used. Unlike the
// time on the clock it does not grow when other programs keep the
// machine busy, which is what makes two benchmark runs comparable.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// peakResident returns the most memory this process has had resident,
// in bytes.
func peakResident() int64 {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return int64(ru.Maxrss) // bytes there, kilobytes elsewhere
	}
	return int64(ru.Maxrss) << 10
}
