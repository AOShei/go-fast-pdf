package gofastpdf_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// TestMain removes temporary files that an earlier run of these tests
// left behind. The tests write a document to a temporary file because
// pkg/loader takes a path; the file is removed when the call returns, but
// not if the process is killed first. Child processes get a temporary
// directory of their own that their parent removes; this covers the
// parent itself and the workers of a fuzzing run.
func TestMain(m *testing.M) {
	if pdftest.ChildID() == "" {
		sweepStaleTempFiles(time.Hour)
	}
	os.Exit(m.Run())
}

// sweepStaleTempFiles deletes this package's own temporary files that
// are older than age. Only names this package creates are matched.
func sweepStaleTempFiles(age time.Duration) {
	for _, pattern := range []string{tempPattern, "gofastpdf-child-*"} {
		matches, _ := filepath.Glob(filepath.Join(os.TempDir(), pattern))
		for _, path := range matches {
			if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > age {
				os.RemoveAll(path)
			}
		}
	}
}

func TestSweepStaleTempFiles(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("TMP", os.Getenv("TMPDIR"))
	t.Setenv("TEMP", os.Getenv("TMPDIR"))
	dir := os.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	write := func(name string, mod time.Time) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(path, mod, mod)
		return path
	}
	stale := write("gofastpdf-123.pdf", old)
	fresh := write("gofastpdf-456.pdf", time.Now())
	other := write("someone-elses-file.pdf", old)
	staleDir := filepath.Join(dir, "gofastpdf-child-789")
	os.Mkdir(staleDir, 0o755)
	os.WriteFile(filepath.Join(staleDir, "inner.pdf"), []byte("x"), 0o644)
	os.Chtimes(staleDir, old, old)

	sweepStaleTempFiles(time.Hour)
	for path, wantGone := range map[string]bool{stale: true, staleDir: true, fresh: false, other: false} {
		if _, err := os.Stat(path); os.IsNotExist(err) != wantGone {
			t.Errorf("%s: gone=%v, want %v", filepath.Base(path), os.IsNotExist(err), wantGone)
		}
	}
}
