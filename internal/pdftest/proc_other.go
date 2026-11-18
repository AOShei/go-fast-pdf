//go:build !unix

package pdftest

import (
	"os/exec"
	"time"
)

// contain is the containment of programs started by the harness. Process
// groups and resource limits as used on Unix have no direct equivalent
// here, so the program is only killed at its time limit, by the context
// it was started with. The reference tools are not installed on these
// systems in CI, and the tests that use them skip.
func contain(cmd *exec.Cmd, maxFileBytes int64, maxCPU time.Duration) (cleanup func()) {
	return func() {}
}
