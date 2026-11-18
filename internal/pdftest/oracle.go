package pdftest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// HavePdftotext reports whether poppler's pdftotext is on the PATH.
func HavePdftotext() bool {
	_, err := exec.LookPath("pdftotext")
	return err == nil
}

// Pdftotext returns the text of each page of the file as poppler extracts
// it. Options are passed before the file name, e.g. "-upw", "secret".
func Pdftotext(path string, options ...string) ([]string, error) {
	args := append([]string{"-enc", "UTF-8"}, options...)
	args = append(args, path, "-")
	out, _, err := Run(2*time.Minute, "pdftotext", args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	// pdftotext ends every page, including the last, with a form feed.
	pages := strings.Split(out, "\f")
	if n := len(pages); n > 0 && strings.TrimSpace(pages[n-1]) == "" {
		pages = pages[:n-1]
	}
	return pages, nil
}

// PdftotextBytes runs Pdftotext on in-memory data. The test is skipped if
// pdftotext is not installed.
func PdftotextBytes(t testing.TB, data []byte, options ...string) []string {
	t.Helper()
	if !HavePdftotext() {
		t.Skip("pdftotext not installed")
	}
	pages, err := Pdftotext(TempFile(t, data), options...)
	if err != nil {
		t.Fatal(err)
	}
	return pages
}

// TempFile writes data to a file in the test's temporary directory and
// returns its path.
func TempFile(t testing.TB, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.pdf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// QpdfCheck reports whether qpdf accepts the file without errors. The
// second result is false when qpdf is not installed.
func QpdfCheck(path string) (err error, available bool) {
	if _, lookErr := exec.LookPath("qpdf"); lookErr != nil {
		return nil, false
	}
	// Exit status 3 means warnings only, which still counts as damaged
	// for a file the builder meant to be well formed.
	if out, _, runErr := Run(time.Minute, "qpdf", "--check", path); runErr != nil {
		return fmt.Errorf("qpdf --check: %v\n%s", runErr, out), true
	}
	return nil, true
}
