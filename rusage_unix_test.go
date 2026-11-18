//go:build unix

package gofastpdf_test

import (
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
