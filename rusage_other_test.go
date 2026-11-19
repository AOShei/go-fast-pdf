//go:build !unix

package gofastpdf_test

import "time"

// cpuTime is not measured on this system.
func cpuTime() time.Duration { return 0 }

// peakResident is not measured on this system.
func peakResident() int64 { return 0 }
