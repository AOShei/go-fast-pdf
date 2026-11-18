//go:build unix

package pdftest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// gone waits for a process to disappear and reports whether it did. A
// process that was killed and not yet reaped still answers signal 0, so
// its state is read from /proc where there is one.
func gone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		if stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
			// The field after the command name is the state; Z is a zombie.
			if i := strings.LastIndexByte(string(stat), ')'); i >= 0 && strings.HasPrefix(string(stat[i+1:]), " Z") {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && convErr == nil {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("no process id in %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// runawayLauncher is a launcher that starts a child and waits for it.
// The child writes to a file without end; when the file-size limit stops
// the writing, the child stays alive, so that only the kill of the
// process group ends it. The child's process id is left in $1/pid.
const runawayLauncher = `( cat /dev/zero > "$1/big" 2>/dev/null; sleep 600 ) & echo $! > "$1/pid"; wait`

// A program run by the harness must not outlive its time limit or fill
// the disk. The stand-in behaves the way LibreOffice did on a hostile
// file in another library of this series: a launcher starts a child, and
// the child writes without end. That wrote 180 GB before it was noticed.
func TestRunStopsALauncherAndItsChild(t *testing.T) {
	const fileLimit = 4 << 20
	dir := t.TempDir()
	start := time.Now()
	_, _, err := runBounded(2*time.Second, fileLimit, 1<<20, "sh", "-c", runawayLauncher, "sh", dir)
	if err == nil || !strings.Contains(err.Error(), "no result after") {
		t.Errorf("a program that never ends returned %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("the call took %v; the time limit was 2s", elapsed)
	}
	// The launcher's child is gone too, not only the launcher.
	pid := readPid(t, filepath.Join(dir, "pid"))
	if !gone(pid, 5*time.Second) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("the launcher's child (pid %d) is still running after the call returned", pid)
	}
	// It could not write past the file-size limit while it ran, and it
	// did reach the limit: writing a few megabytes takes no time.
	info, err := os.Stat(filepath.Join(dir, "big"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > fileLimit || info.Size() < fileLimit/2 {
		t.Errorf("the program wrote %d KB; the limit is %d KB (or half of it, by the shell's block size)", info.Size()>>10, fileLimit>>10)
	}
	size := info.Size()
	time.Sleep(300 * time.Millisecond)
	if again, _ := os.Stat(filepath.Join(dir, "big")); again.Size() != size {
		t.Error("the file is still growing after the call returned")
	}
}

func TestRunFileSizeLimitStopsAWriter(t *testing.T) {
	// One process that writes 64 MB and would then exit normally.
	const fileLimit = 4 << 20
	big := filepath.Join(t.TempDir(), "big")
	start := time.Now()
	_, _, err := runBounded(time.Minute, fileLimit, 1<<20, "sh", "-c", `head -c 67108864 /dev/zero > "$1"`, "sh", big)
	if err == nil {
		t.Error("writing past the limit was not an error")
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("took %v", elapsed)
	}
	if info, statErr := os.Stat(big); statErr != nil || info.Size() > fileLimit {
		t.Errorf("the file is %d KB, limit %d KB (%v)", info.Size()>>10, fileLimit>>10, statErr)
	}
}

func TestRunOutputIsCapped(t *testing.T) {
	// A program that prints far more than is kept, as a tool does with
	// error messages on a damaged file. An earlier version of the corpus
	// test held 35 GB of such output in memory.
	const keep = 64 << 10
	var out, errOut string
	var err error
	n := AllocBytes(func() {
		out, errOut, err = runBounded(time.Minute, 1<<20, keep, "sh", "-c",
			`head -c 8388608 /dev/zero; head -c 8388608 /dev/zero >&2`)
	})
	if err == nil || !strings.Contains(err.Error(), "of output") {
		t.Errorf("output past the cap returned %v", err)
	}
	if len(out) != keep || len(errOut) != toolMaxStderr {
		t.Errorf("kept %d bytes of output and %d of errors; the caps are %d and %d", len(out), len(errOut), keep, toolMaxStderr)
	}
	if n > 16<<20 {
		t.Errorf("allocated %d MB while capturing", n>>20)
	}
	// A program that prints without end is stopped at its time limit,
	// and no more than the caps is kept of what it printed.
	out, errOut, err = runBounded(time.Second, 1<<20, keep, "sh", "-c", `while :; do echo out; echo err >&2; done`)
	if err == nil || !strings.Contains(err.Error(), "no result after") {
		t.Errorf("endless output returned %v", err)
	}
	if len(out) > keep || len(errOut) > toolMaxStderr {
		t.Errorf("kept %d bytes of output and %d of errors; the caps are %d and %d", len(out), len(errOut), keep, toolMaxStderr)
	}
}

// The file-size limit and a file that is opened again and again to
// append a little. Linux holds the limit. macOS was seen not to (CI,
// 2026-11-18: 7.7 MB written against a limit of 4 MB, by two different
// shells), although it holds it for a file kept open, as the tests above
// show. Where the limit does not hold, the time limit and the kill of
// the process group are what bound such a writer.
func TestFileSizeLimitAndAppending(t *testing.T) {
	const fileLimit = 4 << 20
	big := filepath.Join(t.TempDir(), "big")
	script := `while :; do head -c 65536 /dev/zero >> "$1" 2>/dev/null; done`
	if _, _, err := runBounded(2*time.Second, fileLimit, 1<<20, "sh", "-c", script, "sh", big); err == nil {
		t.Error("a program that never ends returned without error")
	}
	info, err := os.Stat(big)
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case info.Size() <= fileLimit:
	case runtime.GOOS == "linux":
		t.Errorf("appending wrote %d KB; the limit is %d KB", info.Size()>>10, fileLimit>>10)
	default:
		t.Logf("on %s appending wrote %d KB past a limit of %d KB: the limit does not hold for appended writes here", runtime.GOOS, info.Size()>>10, fileLimit>>10)
	}
	size := info.Size()
	time.Sleep(300 * time.Millisecond)
	if again, _ := os.Stat(big); again.Size() != size {
		t.Error("the file is still growing after the call returned")
	}
}

func TestRunOrdinaryProgram(t *testing.T) {
	out, errOut, err := Run(10*time.Second, "sh", "-c", `echo hello; echo warning >&2`)
	if err != nil || out != "hello\n" || errOut != "warning\n" {
		t.Errorf("%q, %q, %v", out, errOut, err)
	}
	if _, _, err := Run(10*time.Second, "sh", "-c", `echo problem >&2; exit 3`); err == nil || !strings.Contains(err.Error(), "problem") {
		t.Errorf("a failing program: %v", err)
	}
	if _, _, err := Run(time.Second, "no-such-program-gofastpdf"); err == nil {
		t.Error("a program that is not installed: no error")
	}
	// Arguments reach the program unchanged, whatever they contain.
	odd := `a b "c" $HOME 'd' ; rm -rf x`
	if out, _, err := Run(10*time.Second, "printf", "%s", odd); err != nil || out != odd {
		t.Errorf("argument passed through as %q, %v", out, err)
	}
}

// The processor-time limit is what ends a program when nobody is left to
// enforce the time limit: here it is started with no time limit at all.
func TestProcessorTimeLimitEndsALoop(t *testing.T) {
	// A context that never ends: nothing but the kernel's limit applies.
	cmd := exec.CommandContext(context.Background(), "sh", "-c", `while :; do :; done`)
	cleanup := contain(cmd, 1<<20, time.Second)
	defer cleanup()
	start := time.Now()
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the loop ended without an error")
		}
		if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
			t.Errorf("ended after %v, before it could have used its processor time", elapsed)
		}
	case <-time.After(20 * time.Second):
		t.Error("a loop with a processor-time limit of 2 seconds is still running after 20")
	}
}

// Child test processes: one that hangs after starting a process of its
// own, one that writes temporary files, one whose parent does not stop
// it. Nothing may be left of any of them.
func TestSpawnedChildrenAreContained(t *testing.T) {
	shared := os.Getenv("GOFASTPDF_TEST_SHARED")
	switch ChildID() {
	case "contain/grandchild":
		// Start a long-lived process, record it, then hang.
		cmd := exec.Command("sleep", "600")
		if err := cmd.Start(); err != nil {
			os.Exit(9)
		}
		os.WriteFile(filepath.Join(shared, "pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
		time.Sleep(time.Hour)
	case "contain/tempfiles":
		ChildMain(func() (string, error) {
			f, err := os.CreateTemp("", "gofastpdf-*.pdf")
			if err != nil {
				return "", err
			}
			f.Write(make([]byte, 1<<20))
			f.Close()
			return f.Name(), nil // left behind on purpose
		})
	case "contain/tempfiles-killed":
		f, _ := os.CreateTemp("", "gofastpdf-*.pdf")
		os.WriteFile(filepath.Join(shared, "temp"), []byte(f.Name()), 0o644)
		time.Sleep(time.Hour)
	case "contain/self-stop":
		ChildMain(func() (string, error) {
			time.Sleep(time.Hour)
			return "", nil
		})
	}
	if ChildID() != "" {
		return
	}

	shared = t.TempDir()
	t.Setenv("GOFASTPDF_TEST_SHARED", shared)

	t.Run("a child's own child is killed with it", func(t *testing.T) {
		start := time.Now()
		if _, err := Spawn(t, "contain/grandchild", 2*time.Second); err == nil || !strings.Contains(err.Error(), "hang") {
			t.Errorf("a hanging child returned %v", err)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("took %v", elapsed)
		}
		pid := readPid(t, filepath.Join(shared, "pid"))
		if !gone(pid, 5*time.Second) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("the child's own child (pid %d) outlived it", pid)
		}
	})
	t.Run("temporary files of a child are removed", func(t *testing.T) {
		name, err := Spawn(t, "contain/tempfiles", time.Minute)
		if err != nil || name == "" {
			t.Fatalf("%q, %v", name, err)
		}
		if strings.HasPrefix(name, os.TempDir()+string(os.PathSeparator)+"gofastpdf-") && !strings.Contains(name, "gofastpdf-child-") {
			t.Errorf("the child wrote %s, straight into the shared temporary directory", name)
		}
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("%s is still there after the child returned (%v)", name, err)
		}
	})
	t.Run("temporary files of a killed child are removed", func(t *testing.T) {
		Spawn(t, "contain/tempfiles-killed", 2*time.Second)
		raw, err := os.ReadFile(filepath.Join(shared, "temp"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(string(raw)); !os.IsNotExist(err) {
			t.Errorf("%s is still there after the child was killed (%v)", raw, err)
		}
	})
	t.Run("a child stops itself when its parent does not", func(t *testing.T) {
		start := time.Now()
		_, err := spawn(t, "contain/self-stop", time.Second, false)
		if err == nil || !strings.Contains(err.Error(), "stopped itself") {
			t.Errorf("returned %v", err)
		}
		if elapsed := time.Since(start); elapsed < time.Second || elapsed > time.Second+3*childGrace {
			t.Errorf("took %v; the child's limit was 1s plus %v of grace", elapsed, childGrace)
		}
	})
}

// The shell scripts (testdata/gen.sh, testdata/fetch.sh) contain the
// programs they run with the function in testdata/contain.sh. It is held
// to the same test as the Go runner.
func TestShellContainment(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	lib, err := filepath.Abs(filepath.Join("..", "..", "testdata", "contain.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// run executes a line of shell with the function loaded, under the
	// options the real scripts use.
	run := func(timeout time.Duration, line string, args ...string) (string, string, error) {
		script := `set -euo pipefail; . "$1"; shift; ` + line
		return runBounded(timeout, 64<<20, 1<<20, "bash", append([]string{"-c", script, "bash", lib}, args...)...)
	}

	t.Run("output and exit status pass through", func(t *testing.T) {
		if out, _, err := run(20*time.Second, `contained 10 16 echo hello`); err != nil || out != "hello\n" {
			t.Errorf("%q, %v", out, err)
		}
		if out, _, err := run(20*time.Second, `contained 10 16 sh -c 'exit 7' || echo "status $?"`); err != nil || out != "status 7\n" {
			t.Errorf("%q, %v", out, err)
		}
		dir := t.TempDir()
		if _, _, err := run(20*time.Second, `contained 10 16 printf '%s' "$1" > "$2/out"`, `a b "c" $x`, dir); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "out")); string(got) != `a b "c" $x` {
			t.Errorf("redirected output is %q", got)
		}
		// With "set -e" a failing command still ends the script.
		if out, _, err := run(20*time.Second, `contained 10 16 false; echo not reached`); err == nil || strings.Contains(out, "not reached") {
			t.Errorf("a failing command did not stop the script: %q, %v", out, err)
		}
	})

	t.Run("a launcher and its child are stopped", func(t *testing.T) {
		dir := t.TempDir()
		start := time.Now()
		out, _, err := run(30*time.Second, `contained 2 4 sh -c "$1" sh "$2" || echo "stopped with status $?"`, runawayLauncher, dir)
		if err != nil || !strings.HasPrefix(out, "stopped with status") {
			t.Errorf("%q, %v", out, err)
		}
		if elapsed := time.Since(start); elapsed > 12*time.Second {
			t.Errorf("took %v; the time limit was 2s", elapsed)
		}
		pid := readPid(t, filepath.Join(dir, "pid"))
		if !gone(pid, 5*time.Second) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the launcher's child (pid %d) is still running", pid)
		}
		info, err := os.Stat(filepath.Join(dir, "big"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 4<<20 || info.Size() < 2<<20 {
			t.Errorf("the program wrote %d KB; the limit is 4096 KB (or half, by the shell's block size)", info.Size()>>10)
		}
		size := info.Size()
		time.Sleep(300 * time.Millisecond)
		if again, _ := os.Stat(filepath.Join(dir, "big")); again.Size() != size {
			t.Error("the file is still growing after the call returned")
		}
	})

	t.Run("nothing is left running after a normal return", func(t *testing.T) {
		// The command returns at once but leaves a background process;
		// the watchdog that would enforce the time limit must go too.
		dir := t.TempDir()
		if _, _, err := run(30*time.Second, `contained 600 16 sh -c 'sleep 600 & echo $! > "$1/pid"' sh "$1"`, dir); err != nil {
			t.Fatal(err)
		}
		if pid := readPid(t, filepath.Join(dir, "pid")); !gone(pid, 5*time.Second) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("a process the command left behind (pid %d) is still running", pid)
		}
	})
}
