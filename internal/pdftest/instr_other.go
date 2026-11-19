//go:build !linux

package pdftest

// CountInstructions is not available on this system.
func CountInstructions() (read func() uint64, stop func(), ok bool) {
	return func() uint64 { return 0 }, func() {}, false
}
