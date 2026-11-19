package pdftest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// The defect cases.
//
// Every defect that the review of v0.1.1 found has at least one test
// case, identified by a key such as "B1" or "C1/Tf". While the defects
// were being closed, a register of keys said which cases were still
// expected to fail. All are closed and the register is gone: a case that
// fails is a failing test.

// Defect runs fn as the test for the defect case key. fn returns nil when
// the library behaves correctly and an error describing the wrong
// behaviour otherwise. A panic in fn counts as a failure.
func Defect(t *testing.T, key string, fn func() error) {
	t.Helper()
	settle(t, key, safely(fn))
}

// DefectIsolated is Defect for cases that can take the whole process down
// or never return: a stack overflow, an infinite loop, an allocation the
// runtime cannot satisfy. fn runs in a child process, which is killed
// after the timeout.
func DefectIsolated(t *testing.T, key string, timeout time.Duration, fn func() error) {
	t.Helper()
	settle(t, key, Isolated(t, key, timeout, fn))
}

// Isolated runs fn in a child process and returns its error. A crash of
// any kind, or no result within the timeout, is returned as an error too.
// id must be unique among the Isolated calls of the calling test.
func Isolated(t *testing.T, id string, timeout time.Duration, fn func() error) error {
	t.Helper()
	if os.Getenv(isolatedEnv) == id {
		childMain(fn)
	}
	if os.Getenv(isolatedEnv) != "" {
		return nil // in a child that was started for another id
	}
	return runChild(t, id, timeout)
}

// childMain runs fn in the child process and exits with its verdict.
func childMain(fn func() error) {
	// Fail fast on runaway recursion instead of growing the stack to the
	// default 1 GB limit.
	debug.SetMaxStack(64 << 20)
	// The parent kills the child at the time limit. If the parent is
	// gone, nobody does: the child ends itself a little after it.
	if limit, err := time.ParseDuration(os.Getenv(limitEnv)); err == nil && limit > 0 {
		time.AfterFunc(limit+childGrace, func() {
			fmt.Fprintln(os.Stderr, "the child reached its time limit and was not stopped by its parent")
			os.Exit(exitSelfStopped)
		})
	}
	if err := safely(fn); err != nil {
		fmt.Fprintln(os.Stderr, childMarker+err.Error())
		os.Exit(3)
	}
	os.Exit(0)
}

const (
	isolatedEnv = "GOFASTPDF_ISOLATED"
	childMarker = "DEFECT: "
	// limitEnv tells the child its time limit, so that it can end itself
	// if the parent is no longer there to do it.
	limitEnv   = "GOFASTPDF_CHILD_LIMIT"
	childGrace = 3 * time.Second
	// exitSelfStopped is the exit status of a child that ended itself.
	exitSelfStopped = 5
	// childMaxFileBytes bounds any file a child writes. A child writes
	// the document under test to a temporary file and nothing else.
	childMaxFileBytes = 1 << 30
)

func settle(t *testing.T, key string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", key, err)
	}
}

func safely(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}

func runChild(t *testing.T, key string, timeout time.Duration) error {
	t.Helper()
	_, err := Spawn(t, key, timeout)
	return err
}

// ChildID returns the id this process was started for by Spawn, or the
// empty string in an ordinary test run.
func ChildID() string { return os.Getenv(isolatedEnv) }

// ChildMain runs fn in a process started by Spawn and exits. The string
// fn returns is handed to the parent; it must be a single line.
func ChildMain(fn func() (string, error)) {
	childMain(func() error {
		out, err := fn()
		if err == nil {
			fmt.Fprintln(os.Stdout, resultMarker+out)
		}
		return err
	})
}

const resultMarker = "RESULT: "

// Spawn runs the calling test again in a child process with ChildID set
// to id, waits for it and returns what the child passed to ChildMain. A
// crash of any kind, or no result within the timeout, is an error. It may
// be called from several goroutines.
//
// The child is contained (see contain): it and anything it starts are
// killed at the time limit and on return, and it ends itself if the
// parent dies first. It gets a temporary directory of its own, which is
// removed when Spawn returns, so a child that is killed leaves no files.
func Spawn(t *testing.T, id string, timeout time.Duration) (string, error) {
	t.Helper()
	return spawn(t, id, timeout, true)
}

// spawn is Spawn. With enforce unset the parent does not apply the time
// limit, which lets a test check that the child applies it to itself.
func spawn(t *testing.T, id string, timeout time.Duration, enforce bool) (string, error) {
	t.Helper()
	// Select exactly this test, level by level for subtests.
	parts := strings.Split(t.Name(), "/")
	for i, p := range parts {
		parts[i] = "^" + regexp.QuoteMeta(p) + "$"
	}
	tmp, err := os.MkdirTemp("", "gofastpdf-child-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	parentLimit := timeout
	if !enforce {
		parentLimit = timeout + 10*childGrace
	}
	ctx, cancel := context.WithTimeout(context.Background(), parentLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+strings.Join(parts, "/"), "-test.count=1")
	cmd.Env = append(os.Environ(), isolatedEnv+"="+id, limitEnv+"="+timeout.String(),
		"TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)
	cmd.WaitDelay = time.Second
	// A runaway child can print without end; keep only the tail.
	var tail tailBuffer
	cmd.Stdout, cmd.Stderr = &tail, &tail
	// The processor-time bound is what the child could use by running
	// every core for its whole time limit; only a child that outlives
	// that limit can exceed it.
	cpu := (timeout + 10*time.Second) * time.Duration(runtime.NumCPU())
	cleanup := contain(cmd, childMaxFileBytes, cpu)
	err = cmd.Run()
	cleanup()
	out := tail.buf
	if ctx.Err() != nil {
		return "", fmt.Errorf("no result after %v (hang)", timeout)
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == exitSelfStopped {
		return "", fmt.Errorf("no result after %v (hang; the child stopped itself)", timeout)
	}
	if err != nil {
		return "", errors.New(childFailure(out, err))
	}
	result := ""
	for _, l := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(l, resultMarker); ok {
			result = rest
		}
	}
	return result, nil
}

// tailBuffer keeps the last part of what is written to it.
type tailBuffer struct{ buf []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	const keep = 64 << 10
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*keep {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-keep:]...)
	}
	return len(p), nil
}

// childFailure picks the line of a crashed child's output that says what
// happened.
func childFailure(out []byte, err error) string {
	lines := strings.Split(string(bytes.TrimSpace(out)), "\n")
	for _, l := range lines {
		if rest, ok := strings.CutPrefix(l, childMarker); ok {
			return rest
		}
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "panic:") || strings.HasPrefix(l, "fatal error:") || strings.HasPrefix(l, "runtime:") {
			return strings.TrimSpace(l)
		}
	}
	if n := len(lines); n > 0 && lines[n-1] != "" {
		return fmt.Sprintf("%v: %s", err, lines[n-1])
	}
	return err.Error()
}

// AllocBytes returns the number of heap bytes allocated while fn runs. It
// is meaningful only when nothing else allocates concurrently.
func AllocBytes(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
