package pdftest

import (
	"runtime"
	"testing"
)

// BenchInstructions makes a benchmark report, beside its time, the
// instructions that one run executes, where the system lets them be
// counted. Call it before the loop and what it returns after it:
//
//	defer pdftest.BenchInstructions(b)()
//
// The count is the same on a busy machine, which the time is not; it is
// what bench/compare.sh compares where it is there. The benchmark stays
// on one thread for as long as it runs.
func BenchInstructions(b *testing.B) (done func()) {
	runtime.LockOSThread()
	read, stop, ok := CountInstructions()
	start := read()
	return func() {
		if ok && b.N > 0 {
			b.ReportMetric(float64(read()-start)/float64(b.N), "instr/op")
		}
		stop()
		runtime.UnlockOSThread()
	}
}
