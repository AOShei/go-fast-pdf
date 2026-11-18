package gofastpdf_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// The corpus test scores extraction of every file in testdata/corpus.json
// against a reference and holds the score to testdata/baseline.json.
//
// The reference is the file's known source text when the manifest names
// one ("expect"), and the output of pdftotext otherwise. Files that are
// not on this machine are skipped, so the manifest can list files that
// cannot be redistributed.
//
// A score may rise and may not fall. To record new scores:
//
//	GOFASTPDF_UPDATE=1 go test -run TestCorpus .
//
// which refuses to lower a score; GOFASTPDF_UPDATE=lower allows it, and
// the reason belongs in the commit message.

type corpusFile struct {
	Name string `json:"name"`
	// Path is relative to testdata, or absolute for system files.
	Path string `json:"path"`
	// SHA256 pins the content of a file that is not in the repository.
	SHA256 string `json:"sha256,omitempty"`
	// Expect is a text file, relative to testdata, with the words the
	// document contains.
	Expect string `json:"expect,omitempty"`
	// Tier is A, B or C (IMPLEMENTATION_PLAN.md section 4.3).
	Tier    string `json:"tier"`
	Licence string `json:"licence"`
	// Golden stores this library's own output under testdata/golden.
	Golden bool `json:"golden,omitempty"`
}

type baselineEntry struct {
	F1        float64 `json:"f1"`
	Order     float64 `json:"order"`
	Words     int     `json:"words"`
	WantWords int     `json:"want_words"`
	Pages     int     `json:"pages"`
	Reference string  `json:"reference"`
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func round4(x float64) float64 { return math.Round(x*1e4) / 1e4 }

// tierFloor is the word-level F1 every file of a tier must reach
// (IMPLEMENTATION_PLAN.md, section 4.3). Tier A is text in Latin script
// from the common producers; tier B is other scripts with a ToUnicode
// mapping. Tier C, which needs tables or font programs this library does
// not read yet, has no floor.
var tierFloor = map[string]float64{"A": 0.98, "B": 0.95}

func TestCorpus(t *testing.T) {
	var files []corpusFile
	readJSON(t, "testdata/corpus.json", &files)
	baseline := map[string]baselineEntry{}
	readJSON(t, "testdata/baseline.json", &baseline)
	update := os.Getenv(pdftest.UpdateEnv)
	changed := false

	for _, f := range files {
		t.Run(f.Name, func(t *testing.T) {
			path := f.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join("testdata", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("not on this machine: %s", f.Path)
			}
			if f.SHA256 != "" {
				if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != f.SHA256 {
					t.Skipf("%s is a different version of the file than the one in the manifest", f.Path)
				}
			}

			got, err := extractFile(path, extractOpts{})
			if err != nil {
				t.Fatalf("extraction failed: %v", err)
			}

			var score pdftest.Score
			var reference string
			switch {
			case f.Expect != "":
				want, err := os.ReadFile(filepath.Join("testdata", f.Expect))
				if err != nil {
					t.Fatal(err)
				}
				// The source text has no page breaks: compare as one page.
				score = pdftest.Compare(strings.Join(got.pageTexts(), "\n"), string(want))
				reference = f.Expect
			case pdftest.HavePdftotext():
				want, err := pdftest.Pdftotext(path)
				if err != nil {
					t.Fatal(err)
				}
				score = pdftest.ComparePages(got.pageTexts(), want)
				reference = "pdftotext"
			default:
				t.Skip("pdftotext not installed")
			}
			now := baselineEntry{F1: round4(score.F1), Order: round4(score.Order), Words: score.Got,
				WantWords: score.Want, Pages: len(got.Pages), Reference: reference}
			t.Logf("tier %s: F1 %.4f, order %.4f, %d of %d words, %d pages (against %s)",
				f.Tier, now.F1, now.Order, now.Words, now.WantWords, now.Pages, reference)

			// The floor of the file's tier holds whatever the baseline says.
			if floor, ok := tierFloor[f.Tier]; ok && score.F1 < floor {
				t.Errorf("F1 %.4f is under the floor of tier %s, %.2f", score.F1, f.Tier, floor)
			}

			old, known := baseline[f.Name]
			lower := known && (now.F1 < old.F1-1e-9 || now.Order < old.Order-1e-9)
			switch {
			case update != "" && lower && update != "lower":
				t.Errorf("score fell (F1 %.4f to %.4f, order %.4f to %.4f); %s=lower records it anyway",
					old.F1, now.F1, old.Order, now.Order, pdftest.UpdateEnv)
			case update != "":
				if old != now {
					baseline[f.Name] = now
					changed = true
				}
			case !known:
				t.Errorf("no baseline for this file; run with %s=1", pdftest.UpdateEnv)
			case old.Reference != reference:
				t.Skipf("baseline was recorded against %s, this run compares against %s", old.Reference, reference)
			case lower:
				t.Errorf("score fell below the baseline: F1 %.4f (was %.4f), order %.4f (was %.4f)",
					now.F1, old.F1, now.Order, old.Order)
			}

			if f.Golden {
				pdftest.Golden(t, filepath.Join("testdata", "golden", f.Name+".txt"),
					[]byte(strings.Join(got.pageTexts(), "\n\f\n")+"\n"))
			}
		})
	}

	if changed {
		keys := make([]string, 0, len(baseline))
		for k := range baseline {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		sb.WriteString("{\n")
		for i, k := range keys {
			line, _ := json.Marshal(baseline[k])
			fmt.Fprintf(&sb, "  %q: %s", k, line)
			if i < len(keys)-1 {
				sb.WriteByte(',')
			}
			sb.WriteByte('\n')
		}
		sb.WriteString("}\n")
		if err := os.WriteFile("testdata/baseline.json", []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Invariants that hold for every input (IMPLEMENTATION_PLAN.md section
// 4.2), checked on the fixtures and on any corpus file that is present.
func TestInvariants(t *testing.T) {
	var files []corpusFile
	readJSON(t, "testdata/corpus.json", &files)
	for _, f := range files {
		t.Run(f.Name, func(t *testing.T) {
			path := f.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join("testdata", path)
			}
			if _, err := os.Stat(path); err != nil {
				t.Skipf("not on this machine: %s", f.Path)
			}
			// extractFile already rejects text that is not valid UTF-8.
			one, err := extractFile(path, extractOpts{})
			if err != nil {
				t.Fatalf("extraction failed: %v", err)
			}
			many, err := extractFile(path, extractOpts{Workers: 8})
			if err != nil {
				t.Fatalf("extraction with 8 workers failed: %v", err)
			}
			if diff := sameDocument(one.Doc, many.Doc); diff != "" {
				t.Fatalf("1 worker and 8 workers: %s", diff)
			}
		})
	}
}

// TestExtraCorpus runs the invariants over every PDF below the directories
// in GOFASTPDF_CORPUS (separated like PATH). Each file is read in a child
// process, so that a crash or a hang is one failed file and not the end of
// the run.
func TestExtraCorpus(t *testing.T) {
	dirs := os.Getenv("GOFASTPDF_CORPUS")
	if dirs == "" {
		t.Skip("GOFASTPDF_CORPUS is not set")
	}
	var paths []string
	for _, dir := range filepath.SplitList(dirs) {
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".pdf") {
				paths = append(paths, path)
			}
			return nil
		})
	}
	sort.Strings(paths)
	failed := 0
	for _, path := range paths {
		err := pdftest.Isolated(t, path, 2*time.Minute, func() error {
			// An error is an acceptable answer for a file that cannot be
			// read; invalid UTF-8, a crash and a hang are not.
			if _, err := extractFile(path, extractOpts{}); errors.Is(err, errContract) {
				return err
			}
			return nil
		})
		if err != nil {
			failed++
			t.Errorf("%s: %v", path, err)
		}
	}
	t.Logf("%d files, %d failed", len(paths), failed)
}
