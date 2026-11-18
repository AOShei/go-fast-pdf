//go:build unix

package pdftest

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// contain makes sure a program started by the harness cannot outlive its
// time limit, be left running when the harness dies, or fill the disk,
// whatever input it is given. It returns a function to call when the
// program has returned.
//
//   - The program runs in a process group of its own, and the whole
//     group is killed at the time limit and again on return. A context
//     alone kills only the process it started; a program that works
//     through a child (LibreOffice's launcher does) would leave the
//     child running.
//   - It runs under a file-size limit, so that inside its time limit it
//     cannot write a file larger than maxFileBytes. Shells count the
//     limit in blocks of 512 or of 1024 bytes; the limit is set so that
//     the larger reading gives maxFileBytes and the smaller half of it.
//   - It runs under a processor-time limit. That is what stops it if the
//     harness itself is killed and nobody is left to enforce the time
//     limit: a program that loops is ended by the kernel.
//
// In another process group, the program no longer receives the
// terminal's interrupt. The limits above are what ends it then.
func contain(cmd *exec.Cmd, maxFileBytes int64, maxCPU time.Duration) (cleanup func()) {
	script := fmt.Sprintf(`ulimit -f %d; ulimit -t %d; exec "$@"`, maxFileBytes/1024, int64(maxCPU/time.Second)+1)
	args := append([]string{"sh", "-c", script, "sh", cmd.Path}, cmd.Args[1:]...)
	cmd.Path, cmd.Args = "/bin/sh", args
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	killGroup := func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.Cancel = killGroup
	return func() { killGroup() }
}
