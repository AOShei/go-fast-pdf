//go:build unix

package main

import (
	"syscall"
	"time"
)

// processorTime is the processor time this process has had, user and
// system, on every processor together.
func processorTime() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
