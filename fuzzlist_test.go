package gofastpdf_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEveryFuzzTargetIsRun: every fuzz target of the module is in
// .github/fuzz-targets.txt, which CI fuzzes on each pull request and
// every night, and the list names no target that does not exist.
func TestEveryFuzzTargetIsRun(t *testing.T) {
	data, err := os.ReadFile(".github/fuzz-targets.txt")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) == 2 && !strings.HasPrefix(f[0], "#") {
			listed[filepath.Clean(f[0])+" "+f[1]] = true
		}
	}
	fuzz := regexp.MustCompile(`(?m)^func (Fuzz\w+)\(`)
	found := map[string]bool{}
	filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case d.IsDir() && path != "." && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")):
			return filepath.SkipDir
		case !strings.HasSuffix(path, "_test.go"):
			return nil
		}
		src, _ := os.ReadFile(path)
		for _, m := range fuzz.FindAllStringSubmatch(string(src), -1) {
			found[filepath.Dir(path)+" "+m[1]] = true
		}
		return nil
	})
	for k := range found {
		if !listed[k] {
			t.Errorf("%s is not in .github/fuzz-targets.txt", k)
		}
	}
	for k := range listed {
		if !found[k] {
			t.Errorf(".github/fuzz-targets.txt lists %s, which does not exist", k)
		}
	}
	if len(found) == 0 {
		t.Error("no fuzz target found")
	}
}
