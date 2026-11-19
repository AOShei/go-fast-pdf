package gofastpdf_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// The soak test (IMPLEMENTATION_PLAN.md, Phase 8) reads every PDF in the
// corpus cache: the files of the scored corpus, and files that
// testdata/fetch.sh puts in directories named soak-* for this test
// alone. Those are never scored and nobody looks at what they hold; they
// are there so that the library meets several thousand documents it has
// not seen.
//
// Each file is read in a process of its own, under a time limit and a
// memory ceiling, once with the default options and once with
// DeepHidden, and each result is held to the contract of the shared
// model. The test fails on a crash, on a file that takes too long or too
// much memory, and on a result that breaks the contract. It reports how
// many blocks each check flagged, which goes into the release notes.

// soakOnly starts the names of the cache directories whose files are
// read by the soak test and are not part of the scored corpus.
const soakOnly = "soak-"

const (
	// soakTimeLimit is the time one file may take for both runs and
	// their checks.
	soakTimeLimit = 60 * time.Second
	// soakMemoryCeiling is the memory the process that reads one file
	// may hold: the file, two documents and the checks of the harness.
	soakMemoryCeiling = 1 << 30
)

// soakResult is the outcome for one file. Status is ok, locked, error,
// invalid, crash, timeout or memory.
type soakResult struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Pages  int    `json:"pages,omitempty"`
	Millis int64  `json:"ms,omitempty"`
	PeakMB int64  `json:"peak_mb,omitempty"`
	// Blocks of text, and of those the ones flagged for each reason:
	// with the default options, and with DeepHidden.
	Blocks  int               `json:"blocks,omitempty"`
	Flagged [2]map[string]int `json:"flagged"`
	Any     [2]int            `json:"any"`
	// Warnings counts, by code, the warnings of the run with the default
	// options: those of the document and of its parts.
	Warnings map[string]int `json:"warnings,omitempty"`
}

func TestSoak(t *testing.T) {
	cache := corpusCache()
	if name := pdftest.ChildID(); name != "" {
		pdftest.GuardMemory(soakMemoryCeiling)
		pdftest.ChildMain(func() (string, error) {
			out, _ := json.Marshal(soakFile(filepath.Join(cache, filepath.FromSlash(name))))
			return string(out), nil
		})
	}
	if testing.Short() || pdftest.RaceEnabled {
		t.Skip("the soak test is skipped with -short and under the race detector")
	}
	var names []string
	filepath.WalkDir(cache, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".pdf") {
			rel, _ := filepath.Rel(cache, path)
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if len(names) == 0 {
		t.Skipf("no corpus in %s; run testdata/fetch.sh", cache)
	}
	sort.Strings(names)

	results := make([]soakResult, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(runtime.NumCPU()/2, 1))
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out, err := pdftest.Spawn(t, name, soakTimeLimit)
			switch {
			case err == nil:
				if json.Unmarshal([]byte(out), &results[i]) != nil {
					results[i] = soakResult{Status: "crash", Detail: "no result from the child process"}
				}
			case strings.Contains(err.Error(), "hang"):
				results[i] = soakResult{Status: "timeout", Detail: clipString(err.Error(), 160)}
			case strings.Contains(err.Error(), "memory ceiling"):
				results[i] = soakResult{Status: "memory", Detail: clipString(err.Error(), 160)}
			default:
				results[i] = soakResult{Status: "crash", Detail: clipString(err.Error(), 400)}
			}
		}()
	}
	wg.Wait()

	status := map[string]int{}
	var flagged, any [2]map[string]int
	for m := range flagged {
		flagged[m], any[m] = map[string]int{}, map[string]int{}
	}
	warnings, warned := map[string]int{}, map[string]int{}
	var pages, blocks, soakFiles int
	var slowest, largest int64
	for i, r := range results {
		status[r.Status]++
		if strings.HasPrefix(names[i], soakOnly) {
			soakFiles++
		}
		switch r.Status {
		case "crash", "timeout", "memory", "invalid":
			t.Errorf("%s: %s: %s", names[i], r.Status, r.Detail)
		}
		pages += r.Pages
		blocks += r.Blocks
		for m := range r.Flagged {
			for reason, n := range r.Flagged[m] {
				flagged[m][reason] += n
				any[m][reason]++ // files
			}
		}
		for code, n := range r.Warnings {
			warnings[code] += n
			warned[code]++
		}
		slowest, largest = max(slowest, r.Millis), max(largest, r.PeakMB)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d files (%d of them for this test alone), %d pages, %d blocks of text\n", len(names), soakFiles, pages, blocks)
	fmt.Fprintf(&sb, "outcome:")
	for _, s := range []string{"ok", "locked", "error", "invalid", "crash", "timeout", "memory"} {
		fmt.Fprintf(&sb, " %s %d,", s, status[s])
	}
	// The files are not named: some are of the set that is scored and
	// not looked at, and which of them is slow is a look at it.
	fmt.Fprintf(&sb, "\nslowest file: %d ms of %v allowed\n", slowest, soakTimeLimit)
	fmt.Fprintf(&sb, "most memory: %d MiB resident of %d MiB allowed\n", largest, soakMemoryCeiling>>20)
	fmt.Fprintf(&sb, "\nblocks flagged as hidden, by reason (and the files that have such a block):\n")
	fmt.Fprintf(&sb, "  %-18s %22s %22s\n", "reason", "default options", "with DeepHidden")
	reasons := map[string]bool{}
	for m := range flagged {
		for reason := range flagged[m] {
			reasons[reason] = true
		}
	}
	for _, reason := range sortedKeys(reasons) {
		cell := func(m int) string {
			return fmt.Sprintf("%d (%d files)", flagged[m][reason], any[m][reason])
		}
		fmt.Fprintf(&sb, "  %-18s %22s %22s\n", reason, cell(0), cell(1))
	}
	fmt.Fprintf(&sb, "\nwarnings with the default options, by code (and the files that have it):\n")
	for _, code := range sortedKeys(warnings) {
		fmt.Fprintf(&sb, "  %-32s %7d (%d files)\n", code, warnings[code], warned[code])
	}
	t.Log("\n" + sb.String())
	if out := os.Getenv("GOFASTPDF_SOAK_OUT"); out != "" {
		if err := os.WriteFile(out, []byte(sb.String()), 0o644); err != nil {
			t.Error(err)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// soakFile runs in the child process.
func soakFile(path string) soakResult {
	start := time.Now()
	var r soakResult
	var docs [2]*gofastcore.Document
	for m, deep := range []bool{false, true} {
		got, err := extractFile(path, extractOpts{Deep: deep, Images: true})
		switch {
		case err == nil:
			docs[m] = got.Doc
		case errors.Is(err, errContract):
			return soakResult{Status: "invalid", Detail: clipString(err.Error(), 200)}
		case errors.Is(err, gofastcore.ErrPasswordRequired):
			return soakResult{Status: "locked"}
		default:
			return soakResult{Status: "error", Detail: clipString(err.Error(), 200)}
		}
	}
	r.Status, r.Pages = "ok", len(docs[0].Parts)
	r.Warnings = map[string]int{}
	for _, w := range docs[0].Warnings {
		r.Warnings[w.Code]++
	}
	for m, doc := range docs {
		r.Flagged[m] = map[string]int{}
		for i := range doc.Parts {
			p := &doc.Parts[i]
			if m == 0 {
				for _, w := range p.Warnings {
					r.Warnings[w.Code]++
				}
			}
			for j := range p.Blocks {
				b := &p.Blocks[j]
				if b.Kind == gofastcore.KindImage {
					continue
				}
				if m == 0 {
					r.Blocks++
				}
				if b.Hidden == 0 {
					continue
				}
				r.Any[m]++
				for _, reason := range strings.Split(b.Hidden.String(), "+") {
					r.Flagged[m][reason]++
				}
			}
		}
	}
	r.Millis = time.Since(start).Milliseconds()
	r.PeakMB = peakResident() >> 20
	return r
}
