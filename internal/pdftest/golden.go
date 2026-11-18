package pdftest

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// UpdateEnv is the environment variable that makes Golden and the corpus
// baseline rewrite their files instead of comparing against them:
//
//	GOFASTPDF_UPDATE=1 go test ./...
//
// An environment variable is used and not a flag because a flag would have
// to be defined in every test binary of the module.
const UpdateEnv = "GOFASTPDF_UPDATE"

// Updating reports whether golden files are being rewritten.
func Updating() bool { return os.Getenv(UpdateEnv) != "" }

// Golden compares got with the file at path (relative to the test's
// package directory). When updating, it writes the file instead. A changed
// golden file shows up in the diff of the change and is reviewed there.
func Golden(t testing.TB, path string, got []byte) {
	t.Helper()
	if Updating() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file missing (run with %s=1 to create it): %v", UpdateEnv, err)
	}
	// Tolerate a checkout that converted line endings.
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from %s (run with %s=1 to update, then review the diff)\n%s",
			path, UpdateEnv, firstDifference(got, want))
	}
}

func firstDifference(got, want []byte) string {
	gl, wl := bytes.Split(got, []byte("\n")), bytes.Split(want, []byte("\n"))
	for i := 0; i < len(gl) || i < len(wl); i++ {
		var g, w []byte
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if !bytes.Equal(g, w) {
			return "first difference at line " + strconv.Itoa(i+1) + ":\n  got:  " + clip(g) + "\n  want: " + clip(w)
		}
	}
	return ""
}

func clip(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}
