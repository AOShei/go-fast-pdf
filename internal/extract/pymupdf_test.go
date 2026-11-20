package extract

import (
	"crypto/sha256"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// traceKey is what the two readers must agree on for a glyph: whether
// its render mode paints it, and its opacity, which decide no_ink.
type traceKey struct {
	painted bool
	opacity uint8 // in tenths
}

// TestAgainstPyMuPDF compares what each run's display depends on, as the
// file declares it, with PyMuPDF's text trace, glyph count by glyph
// count, on the fixtures and
// on a sample of the public corpus (the fixtures only with -short). It needs the benchmark environment
// (../bench-env, or the directory named by GOFASTPDF_BENCH_ENV) and is
// skipped without it.
func TestAgainstPyMuPDF(t *testing.T) {
	env := os.Getenv("GOFASTPDF_BENCH_ENV")
	if env == "" {
		env = filepath.Join("..", "..", "..", "bench-env")
	}
	python := filepath.Join(env, ".venv", "bin", "python")
	if _, err := os.Stat(python); err != nil {
		t.Skip("no benchmark environment")
	}
	script, _ := filepath.Abs(filepath.Join("testdata", "texttrace.py"))

	var paths []string
	fixtures, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "fixtures", "*.pdf"))
	for _, p := range fixtures {
		if !strings.Contains(filepath.Base(p), "enc-") {
			paths = append(paths, p)
		}
	}
	required := len(paths)
	if !testing.Short() {
		// A sample of the corpus, chosen by hash so that it is the same
		// on every run.
		home, _ := os.UserHomeDir()
		corpus, _ := filepath.Glob(filepath.Join(home, ".cache", "gofastpdf-corpus", "*", "*.pdf"))
		sort.Strings(corpus)
		for _, p := range corpus {
			if sum := sha256.Sum256([]byte(filepath.Base(p))); sum[0] < 40 {
				paths = append(paths, p)
			}
		}
	}

	compared, agree, total := 0, 0, 0
	// The files that are in the cache for the soak test alone are
	// counted apart: see the end.
	soakFiles, soakTotal, soakAgree := 0, 0, 0
	var worst []string
	for i, path := range paths {
		soak := strings.HasPrefix(filepath.Base(filepath.Dir(path)), "soak-")
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 8<<20 {
			continue
		}
		f, err := file.Open(data, file.Limits{})
		if err != nil {
			continue
		}
		out, _, err := pdftest.Run(time.Minute, python, "-I", script, path)
		if err != nil {
			continue // PyMuPDF does not read the file
		}
		var theirs [][][7]float64
		if json.Unmarshal([]byte(out), &theirs) != nil {
			continue
		}
		pages := f.Pages()
		if len(pages) != len(theirs) || len(pages) > 50 {
			continue
		}
		a, b := map[traceKey]int{}, map[traceKey]int{}
		for n := range pages {
			res := Page(f, &pages[n], Options{Runs: true})
			for _, r := range res.Runs {
				if r.OptionalOff || r.Font == nil || r.Font.Type3 {
					continue // PyMuPDF traces neither
				}
				// Stroked text has the stroke's opacity.
				alpha := r.FillAlpha
				if r.Mode%4 == 1 {
					alpha = r.StrokeAlpha
				}
				k := traceKey{painted: r.Mode%4 != 3, opacity: uint8(math.Round(float64(alpha) * 10))}
				a[k] += int(r.Glyphs)
			}
			for _, s := range theirs[n] {
				k := traceKey{painted: s[1] != 3, opacity: uint8(math.Round(s[5] * 10))}
				b[k] += int(s[0])
			}
		}
		na, nb, same := 0, 0, 0
		for k, v := range a {
			na += v
			same += min(v, b[k])
		}
		for _, v := range b {
			nb += v
		}
		if na == 0 || nb == 0 {
			continue
		}
		if soak {
			soakFiles++
			soakTotal += max(na, nb)
			soakAgree += same
			continue
		}
		compared++
		share := float64(same) / float64(max(na, nb))
		total += max(na, nb)
		agree += same
		if share < 0.95 {
			worst = append(worst, filepath.Base(path))

			if i < required {
				t.Errorf("%s: the two readers agree on %.1f%% of the glyphs (%d and %d)\n ours   %v\n theirs %v", filepath.Base(path), 100*share, na, nb, a, b)
			}
		}
	}
	if compared == 0 {
		t.Skip("nothing to compare")
	}
	overall := float64(agree) / float64(total)
	t.Logf("%d files compared, %d glyphs: render mode and opacity agree for %.2f%% of the glyphs; %d files under 95%%: %s",
		compared, total, 100*overall, len(worst), strings.Join(worst, " "))
	// Where the two differ, the cause is known and is not a different
	// reading of the page's state: PyMuPDF reports text that is both
	// filled and stroked twice.
	if overall < 0.95 {
		t.Errorf("agreement is %.2f%% of the glyphs, under 95%%", 100*overall)
	}

	// The files of the soak test are documents that nothing here was
	// developed against: a bound on them too.
	if soakFiles > 0 {
		state := float64(soakAgree) / float64(soakTotal)
		t.Logf("%d files of the soak set, %d glyphs: render mode and opacity agree for %.2f%% of the glyphs", soakFiles, soakTotal, 100*state)
		if state < 0.98 {
			t.Errorf("on the files of the soak set, render mode and opacity agree for %.2f%% of the glyphs, under 98%%", 100*state)
		}
	}
}
