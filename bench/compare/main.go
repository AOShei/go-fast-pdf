// Command compare measures this library beside other programs that
// extract text from PDF files, on the same files, and writes the table
// that the README shows.
//
//	go run ./bench/compare                  measure, write bench/comparison.md
//	go run ./bench/compare -readme README.md   and put the summary into the README
//
// What it needs, beyond this module:
//
//   - testdata/fetch.sh has been run, for the documents;
//   - pdftotext (poppler) on the path;
//   - a Python with PyMuPDF, pypdfium2, pypdf and pdfplumber, which is
//     ../bench-env/.venv/bin/python unless -python names another
//     a virtual environment with those packages installed by pip;
//   - the program of bench/tools/rust, which times the Rust libraries
//     pdf_oxide, pdf-extract and lopdf, built with
//     CARGO_TARGET_DIR=../../../../bench-env/rust-target cargo build --release
//     in that directory: ../bench-env/rust-target/release/pdfbench-rust
//     unless -rust names another.
//
// A program that is not there is left out of the table and named as
// missing. Nothing here is imported by the library, and the Python
// libraries are not dependencies of this module: they are programs that
// this one starts.
//
// Every program is started through the contained runner of
// internal/pdftest: with a time limit, in a process group that is killed
// when the limit is reached, and unable to write a large file.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// result is what one program did with one document.
type result struct {
	Seconds float64 `json:"seconds"`
	Chars   int     `json:"chars"`
	Pages   int     `json:"pages"`
	Runs    int     `json:"runs"`
	Version string  `json:"version"`
	Missing string  `json:"missing,omitempty"`
	// Peak is the most memory the process had resident, in bytes.
	Peak int64  `json:"-"`
	Err  string `json:"-"`
}

// tool is one program in the comparison.
type tool struct {
	name string
	// note says what is timed.
	note string
	run  func(path string) result
}

type document struct {
	name  string // as shown
	path  string
	pages int
	bytes int64
}

var (
	pythonPath = flag.String("python", filepath.Join("..", "bench-env", ".venv", "bin", "python"), "the Python that has the other libraries")
	rustPath   = flag.String("rust", filepath.Join("..", "bench-env", "rust-target", "release", "pdfbench-rust"), "the program that times the Rust libraries (bench/tools/rust)")
	cacheDir   = flag.String("cache", "", "the corpus cache (default: $GOFASTPDF_CACHE, or gofastpdf-corpus in the user's cache directory)")
	repeats    = flag.Int("repeats", 5, "runs of each program on each document; the best counts")
	budget     = flag.Duration("budget", 20*time.Second, "stop repeating a program on a document after this much time")
	limit      = flag.Duration("limit", 10*time.Minute, "the time one program may take for all its runs on one document")
	outPath    = flag.String("out", filepath.Join("bench", "comparison.md"), "where the report is written")
	readmePath = flag.String("readme", "", "a file in which the text between the comparison markers is replaced by the summary")
	only       = flag.String("only", "", "measure only the programs whose name contains this")
	self       = flag.String("self", "", "internal: time this library on a file and print the result")
	workers    = flag.Int("workers", 1, "internal: with -self, the workers")
)

func main() {
	flag.Parse()
	if *self != "" {
		timeSelf(*self, *workers)
		return
	}
	if err := compare(); err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		os.Exit(1)
	}
}

// timeSelf is the child that measures this library: it reads the file
// and extracts it, as a caller does, and prints the best time.
func timeSelf(path string, workers int) {
	var r result
	var spent time.Duration
	for r.Runs < *repeats && (r.Runs == 0 || spent < *budget) {
		start := time.Now()
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Workers: workers})
		took := time.Since(start)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// The text is made, as the other programs make it.
		r.Chars = len([]rune(doc.Text()))
		r.Pages = len(doc.Parts)
		if s := took.Seconds(); r.Runs == 0 || s < r.Seconds {
			r.Seconds = s
		}
		spent += took
		r.Runs++
	}
	out, _ := json.Marshal(r)
	fmt.Println(string(out))
}

func compare() error {
	docs, err := documents()
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "gofastpdf-compare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	command := filepath.Join(work, "gofastpdf")
	if out, err := exec.Command("go", "build", "-o", command, "./cmd/gofastpdf").CombinedOutput(); err != nil {
		return fmt.Errorf("building the command: %v: %s", err, out)
	}
	me, err := os.Executable()
	if err != nil {
		return err
	}

	tools := []tool{
		{name: "go-fast-pdf", note: "library, one worker", run: func(path string) result {
			return child(me, "-self", path, "-workers", "1", "-repeats", strconv.Itoa(*repeats), "-budget", budget.String())
		}},
		{name: "go-fast-pdf, all processors", note: fmt.Sprintf("library, %d workers", runtime.NumCPU()), run: func(path string) result {
			return child(me, "-self", path, "-workers", strconv.Itoa(runtime.NumCPU()), "-repeats", strconv.Itoa(*repeats), "-budget", budget.String())
		}},
		{name: "gofastpdf (the command)", note: "command, text output, one worker; the time of the process", run: func(path string) result {
			return process(func() string { return "" }, command, "-format", "text", "-workers", "1", "-quiet", path)
		}},
		{name: "pdftotext (poppler)", note: "command; the time of the process", run: func(path string) result {
			return process(func() string { return version("pdftotext", "-v") }, "pdftotext", "-enc", "UTF-8", path, "-")
		}},
	}
	script := filepath.Join("bench", "tools", "extract.py")
	for _, lib := range [][2]string{{"PyMuPDF", "pymupdf"}, {"pypdfium2 (PDFium)", "pypdfium2"}, {"pypdf", "pypdf"}, {"pdfplumber", "pdfplumber"}} {
		tools = append(tools, tool{name: lib[0], note: "Python library; the time of its calls, without starting Python", run: func(path string) result {
			return child(*pythonPath, "-I", script, lib[1], path, strconv.Itoa(*repeats), strconv.FormatFloat(budget.Seconds(), 'f', 1, 64))
		}})
	}
	for _, lib := range []string{"pdf_oxide", "pdf-extract", "lopdf"} {
		tools = append(tools, tool{name: lib + " (Rust)", note: "Rust library; the time of its calls, without starting the program", run: func(path string) result {
			return child(*rustPath, lib, path, strconv.Itoa(*repeats), strconv.FormatFloat(budget.Seconds(), 'f', 1, 64))
		}})
	}

	// results[t][d]
	results := make([][]result, len(tools))
	var kept []int
	for t, tl := range tools {
		if *only != "" && !strings.Contains(tl.name, *only) && tl.name != "go-fast-pdf" {
			continue
		}
		results[t] = make([]result, len(docs))
		missing := ""
		for d, doc := range docs {
			r := tl.run(doc.path)
			results[t][d] = r
			if r.Missing != "" {
				missing = r.Missing
				break
			}
			status := fmt.Sprintf("%8.1f ms", r.Seconds*1000)
			if r.Err != "" {
				status = r.Err
			}
			fmt.Fprintf(os.Stderr, "%-28s %-28s %s\n", tl.name, doc.name, status)
		}
		if missing != "" {
			fmt.Fprintf(os.Stderr, "%-28s not installed: %s\n", tl.name, missing)
			results[t] = nil
			continue
		}
		kept = append(kept, t)
	}

	report, summary := render(tools, kept, docs, results)
	if err := os.WriteFile(*outPath, []byte(report), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "wrote", *outPath)
	if *readmePath != "" {
		old, err := os.ReadFile(*readmePath)
		if err != nil {
			return err
		}
		updated, err := between(string(old), summary)
		if err != nil {
			return fmt.Errorf("%s: %v", *readmePath, err)
		}
		if err := os.WriteFile(*readmePath, []byte(updated), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "updated", *readmePath)
	}
	return nil
}

// child runs a program that times itself and prints a result.
func child(name string, args ...string) result {
	out, _, peak, err := pdftest.RunMeasured(*limit, name, args...)
	if errors.Is(err, exec.ErrNotFound) {
		return result{Missing: err.Error()}
	}
	if err != nil {
		return result{Err: "failed: " + clip(err.Error())}
	}
	var r result
	line := strings.TrimSpace(out)
	if i := strings.LastIndexByte(line, '\n'); i >= 0 {
		line = line[i+1:]
	}
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return result{Err: "no result"}
	}
	r.Peak = peak
	return r
}

// process times a command as a whole: from its start to its end.
func process(version func() string, name string, args ...string) result {
	var r result
	var spent time.Duration
	for r.Runs < *repeats && (r.Runs == 0 || spent < *budget) {
		out, took, peak, err := pdftest.RunMeasured(*limit, name, args...)
		if errors.Is(err, exec.ErrNotFound) {
			return result{Missing: err.Error()}
		}
		if err != nil {
			return result{Err: "failed: " + clip(err.Error())}
		}
		if s := took.Seconds(); r.Runs == 0 || s < r.Seconds {
			r.Seconds = s
		}
		r.Chars, r.Peak = len([]rune(out)), max(r.Peak, peak)
		spent += took
		r.Runs++
	}
	r.Version = version()
	return r
}

func version(name, arg string) string {
	stdout, stderr, _ := pdftest.Run(10*time.Second, name, arg)
	line, _, _ := strings.Cut(strings.TrimSpace(stdout+stderr), "\n")
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "?"
	}
	return fields[len(fields)-1]
}

// commit names the tree that was measured.
func commit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "?"
	}
	v := "commit " + strings.TrimSpace(string(out))
	if exec.Command("git", "diff", "--quiet", "HEAD", "--", ".", ":!README.md", ":!bench/comparison.md").Run() != nil {
		v += ", modified"
	}
	return v
}

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// documents are the files that are measured: one small document that is
// in the repository, and the long documents of the tuning set of the
// public corpus, chosen by what the manifest says of them and by
// nothing else: 30 pages or more, not encrypted, not damaged, at most
// two from one family of producers, the longest first.
func documents() ([]document, error) {
	docs := []document{{name: "english.pdf (in the repository)", path: filepath.Join("testdata", "fixtures", "english.pdf"), pages: 3}}
	cache := *cacheDir
	if cache == "" {
		cache = os.Getenv("GOFASTPDF_CACHE")
	}
	if cache == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		cache = filepath.Join(base, "gofastpdf-corpus")
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "corpus-public.json"))
	if err != nil {
		return nil, fmt.Errorf("run this from the root of the module: %v", err)
	}
	var manifest []struct {
		Name      string   `json:"name"`
		Size      int64    `json:"size"`
		Set       string   `json:"set"`
		Pages     int      `json:"pages"`
		Producer  string   `json:"producer"`
		Flags     []string `json:"flags"`
		Reference bool     `json:"reference"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	sort.SliceStable(manifest, func(i, j int) bool {
		if manifest[i].Pages != manifest[j].Pages {
			return manifest[i].Pages > manifest[j].Pages
		}
		return manifest[i].Name < manifest[j].Name
	})
	perProducer := map[string]int{}
	for _, f := range manifest {
		flags := strings.Join(f.Flags, " ")
		if f.Set != "tune" || !f.Reference || f.Pages < 30 || strings.Contains(flags, "encrypted") || strings.Contains(flags, "damaged") || perProducer[f.Producer] >= 2 {
			continue
		}
		path := filepath.Join(cache, filepath.FromSlash(f.Name))
		if _, err := os.Stat(path); err != nil {
			continue // not fetched
		}
		perProducer[f.Producer]++
		docs = append(docs, document{name: f.Name, path: path, pages: f.Pages, bytes: f.Size})
	}
	if info, err := os.Stat(docs[0].path); err == nil {
		docs[0].bytes = info.Size()
	} else {
		return nil, fmt.Errorf("run this from the root of the module: %v", err)
	}
	if len(docs) == 1 {
		fmt.Fprintf(os.Stderr, "no documents of the public corpus in %s: run testdata/fetch.sh; measuring the small document only\n", cache)
	}
	return docs, nil
}

const (
	beginMarker = "<!-- comparison:begin (written by go run ./bench/compare -readme README.md) -->"
	endMarker   = "<!-- comparison:end -->"
)

// between replaces what is between the markers of a text.
func between(text, summary string) (string, error) {
	i, j := strings.Index(text, beginMarker), strings.Index(text, endMarker)
	if i < 0 || j < i {
		return "", errors.New("the comparison markers are not there")
	}
	return text[:i+len(beginMarker)] + "\n" + summary + text[j:], nil
}

// render writes the report and the summary for the README.
func render(tools []tool, kept []int, docs []document, results [][]result) (report, summary string) {
	// Totals over the documents that every program read.
	readByAll := make([]bool, len(docs))
	for d := range docs {
		readByAll[d] = true
		for _, t := range kept {
			if results[t][d].Err != "" {
				readByAll[d] = false
			}
		}
	}
	type total struct {
		seconds float64
		pages   int
		peak    int64
		failed  int
	}
	totals := make([]total, len(tools))
	for _, t := range kept {
		for d := range docs {
			r := results[t][d]
			if r.Err != "" {
				totals[t].failed++
				continue
			}
			totals[t].peak = max(totals[t].peak, r.Peak)
			if readByAll[d] {
				totals[t].seconds += r.Seconds
				totals[t].pages += docs[d].pages
			}
		}
	}

	var sum strings.Builder
	fmt.Fprintf(&sum, "| | Pages per second | Time against go-fast-pdf | Most memory |\n|---|---:|---:|---:|\n")
	base := totals[kept[0]].seconds
	for _, t := range kept {
		tt := totals[t]
		against := "1"
		if t != kept[0] {
			against = fmt.Sprintf("%.2f times", tt.seconds/base)
			if tt.seconds/base >= 10 {
				against = fmt.Sprintf("%s times", number(tt.seconds/base))
			}
		}
		peak := "-"
		if tt.peak > 0 {
			peak = fmt.Sprintf("%d MB", tt.peak>>20)
		}
		name := tools[t].name
		if tt.failed > 0 {
			name += fmt.Sprintf(" (failed on %d)", tt.failed)
		}
		fmt.Fprintf(&sum, "| %s | %s | %s | %s |\n", name, thousands(float64(tt.pages)/tt.seconds), against, peak)
	}
	pages, size, counted := 0, int64(0), 0
	for d, doc := range docs {
		if readByAll[d] {
			pages += doc.pages
			size += doc.bytes
			counted++
		}
	}
	fmt.Fprintf(&sum, "\n%d documents, %d pages, %.1f MB: the long documents of the public corpus's tuning set and one small one.\n", counted, pages, float64(size)/(1<<20))
	fmt.Fprintf(&sum, "Each program reads each document %d times and its best time counts. Measured on %s with %s, %s.\n",
		*repeats, machine(), runtime.Version(), time.Now().UTC().Format("2006-01-02"))
	fmt.Fprintf(&sum, "The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).\n")

	var sb strings.Builder
	sb.WriteString("# Comparison with other PDF text extractors\n\n")
	sb.WriteString("Written by `go run ./bench/compare`. Do not edit: run it again.\n\n")
	sb.WriteString(sum.String())
	sb.WriteString("\n## What is measured\n\n")
	sb.WriteString("Each program reads a file from the disk and gives the text of all its pages.\n\n")
	sb.WriteString("| Program | Version | What is timed |\n|---|---|---|\n")
	for _, t := range kept {
		v := commit() // this library, which has no other version before its release
		for _, r := range results[t] {
			if r.Version != "" {
				v = r.Version
				break
			}
		}
		fmt.Fprintf(&sb, "| %s | %s | %s |\n", tools[t].name, v, tools[t].note)
	}
	sb.WriteString(`
- The libraries are timed inside a process that has already started: the time is that of opening the file
  and extracting every page. The two commands are timed as processes, start to end, which adds about a
  millisecond to each of their runs.
- go-fast-pdf does more than give text: it builds the document of the shared model, with blocks in reading
  order, their boxes, headings, lists and tables. The others are asked for plain text.
- The other programs read a page at a time on one thread, so the row to compare them with is the one with
  one worker.
- Memory is the most that the process had resident, the interpreter included for the Python libraries.
- The documents are chosen by a rule, from what the manifest of the corpus says of them and nothing else:
  the tuning set, 30 pages or more, not encrypted, not damaged, at most two from one family of producers,
  the longest first. No document is chosen for how a program does on it.
- A time that another job on the machine disturbed is too long, never too short; the best of the runs is
  the one disturbed least.

## Each document

Milliseconds, best run.

`)
	sb.WriteString("| Document | Pages |")
	for _, t := range kept {
		fmt.Fprintf(&sb, " %s |", tools[t].name)
	}
	sb.WriteString("\n|---|---:|")
	for range kept {
		sb.WriteString("---:|")
	}
	sb.WriteString("\n")
	for d, doc := range docs {
		fmt.Fprintf(&sb, "| %s | %d |", doc.name, doc.pages)
		for _, t := range kept {
			r := results[t][d]
			if r.Err != "" {
				fmt.Fprintf(&sb, " %s |", r.Err)
			} else {
				fmt.Fprintf(&sb, " %s |", number(r.Seconds*1000))
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nCharacters of text, as each program counts its own output:\n\n| Document |")
	for _, t := range kept {
		fmt.Fprintf(&sb, " %s |", tools[t].name)
	}
	sb.WriteString("\n|---|")
	for range kept {
		sb.WriteString("---:|")
	}
	sb.WriteString("\n")
	for d, doc := range docs {
		fmt.Fprintf(&sb, "| %s |", doc.name)
		for _, t := range kept {
			if r := results[t][d]; r.Err != "" {
				sb.WriteString(" - |")
			} else {
				fmt.Fprintf(&sb, " %d |", r.Chars)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String(), sum.String()
}

// number writes a value with three figures that count.
func number(v float64) string {
	switch {
	case v >= 100:
		return thousands(v)
	case v >= 10:
		return strconv.FormatFloat(v, 'f', 1, 64)
	case v >= 1:
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
	return strconv.FormatFloat(v, 'f', 3, 64)
}

// thousands writes a whole number with commas.
func thousands(v float64) string {
	s := strconv.FormatInt(int64(v+0.5), 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func machine() string {
	cpu := ""
	if raw, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
				cpu = strings.TrimSpace(v)
				break
			}
		}
	}
	if cpu == "" {
		cpu = runtime.GOARCH
	}
	return fmt.Sprintf("%s (%d threads, %s/%s)", cpu, runtime.NumCPU(), runtime.GOOS, runtime.GOARCH)
}
