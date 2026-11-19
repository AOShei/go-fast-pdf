package pdftest

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Bounds for the reference tools (pdftotext, pdfinfo, qpdf). They write
// to standard output; none of them has a reason to write a file at all.
const (
	toolMaxFileBytes = 64 << 20
	toolMaxStdout    = 64 << 20
	toolMaxStderr    = 1 << 20
)

// Run runs an external program with a time limit and returns what it
// wrote to standard output and standard error.
//
// The program is contained (see contain): it and anything it starts are
// killed at the time limit, it cannot write a large file, and its output
// is kept only up to a cap. Going over the time limit or the output cap
// is an error.
func Run(timeout time.Duration, name string, args ...string) (stdout, stderr string, err error) {
	return RunCapped(timeout, toolMaxStdout, name, args...)
}

// RunCapped is Run with a cap on standard output chosen by the caller.
func RunCapped(timeout time.Duration, maxStdout int, name string, args ...string) (stdout, stderr string, err error) {
	return runBounded(timeout, toolMaxFileBytes, maxStdout, name, args...)
}

// RunMeasured is Run for a harness that compares programs: it also says
// how long the program ran and the most memory it had resident, which is
// 0 where the system does not tell.
func RunMeasured(timeout time.Duration, name string, args ...string) (stdout string, took time.Duration, peakBytes int64, err error) {
	var m measured
	stdout, _, err = runMeasured(&m, timeout, toolMaxFileBytes, toolMaxStdout, name, args...)
	return stdout, m.took, m.peak, err
}

type measured struct {
	took time.Duration
	peak int64
}

func runBounded(timeout time.Duration, maxFileBytes int64, maxStdout int, name string, args ...string) (string, string, error) {
	return runMeasured(nil, timeout, maxFileBytes, maxStdout, name, args...)
}

func runMeasured(m *measured, timeout time.Duration, maxFileBytes int64, maxStdout int, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if cmd.Err != nil {
		return "", "", cmd.Err // the program is not installed
	}
	so, se := &cappedBuffer{limit: maxStdout}, &cappedBuffer{limit: toolMaxStderr}
	cmd.Stdout, cmd.Stderr = so, se
	// If something the program started keeps the output pipes open after
	// the program is gone, do not wait for it.
	cmd.WaitDelay = time.Second
	cleanup := contain(cmd, maxFileBytes, timeout+10*time.Second)
	start := time.Now()
	err := cmd.Run()
	if m != nil {
		m.took, m.peak = time.Since(start), peakResident(cmd.ProcessState)
	}
	cleanup()
	switch {
	case ctx.Err() != nil:
		err = fmt.Errorf("%s: no result after %v", name, timeout)
	case err != nil:
		err = fmt.Errorf("%s: %v: %s", name, err, firstLine(se.sb.String()))
	case so.full:
		err = fmt.Errorf("%s: more than %d KB of output", name, maxStdout>>10)
	}
	return so.sb.String(), se.sb.String(), err
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// cappedBuffer keeps the first limit bytes written to it and discards
// the rest. On a damaged file a tool can print error messages without
// end, and the harness runs many tools at once.
type cappedBuffer struct {
	sb    strings.Builder
	limit int
	full  bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.sb.Len(); room >= len(p) {
		c.sb.Write(p)
	} else {
		c.sb.Write(p[:max(room, 0)])
		c.full = true
	}
	return len(p), nil
}
