//go:build unix

package gofastpdf

import (
	"syscall"
	"time"
)

// processorTime returns the processor time this process has used.
func processorTime() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
