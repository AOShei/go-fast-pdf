package main

// The settings of Go's garbage collector (go run ./bench/compare -gc).
//
// The library leaves the collector's settings alone: they are the
// program's, for the whole process. A program can give the collector
// more memory to work with, GOGC above 100 or a memory limit, and spend
// less time collecting. This measures what each setting costs and
// gives, on three ways of reading documents, each setting in a process
// of its own, and writes bench/gc.md and, with -readme, the summary of
// the README.

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

var (
	gcMode     = flag.Bool("gc", false, "measure the settings of the garbage collector instead, and write -gc-out")
	gcSettings = flag.String("gc-settings", "GOGC=100;GOGC=200;GOGC=400", "the settings measured, separated by semicolons; each is one or more NAME=value, and the first is the one the others are compared with")
	gcOut      = flag.String("gc-out", filepath.Join("bench", "gc.md"), "where the report of -gc is written")
	batchList  = flag.String("batch", "", "internal: read the files listed in this file, one path a line, and print the result")
	parallel   = flag.Int("parallel", 1, "internal: with -batch, the documents read at once")
	rounds     = flag.Int("rounds", 5, "with -gc, the processes for each setting on each workload, the settings taking turns")
)

const (
	gcBeginMarker = "<!-- gc:begin (written by go run ./bench/compare -gc -readme README.md) -->"
	gcEndMarker   = "<!-- gc:end -->"
)

// batchResult is what one process did with a batch of documents.
type batchResult struct {
	// Seconds is the best time of a run, and CPU the processor time of
	// that run, in seconds.
	Seconds float64 `json:"seconds"`
	CPU     float64 `json:"cpu"`
	Runs    int     `json:"runs"`
	Docs    int     `json:"docs"`
	Pages   int     `json:"pages"`
	Failed  int     `json:"failed"`
	// Cycles is the number of collections in a run.
	Cycles float64 `json:"cycles"`
	// Peak is the most memory the process had resident; over several
	// processes, the median, and PeakLow and PeakHigh the least and the
	// most.
	Peak      int64  `json:"-"`
	PeakLow   int64  `json:"-"`
	PeakHigh  int64  `json:"-"`
	Processes int    `json:"-"`
	Err       string `json:"-"`
}

// workload is a way of reading documents.
type workload struct {
	name     string
	short    string // for the summary
	paths    []string
	pages    int
	bytes    int64
	workers  int // Options.Workers
	parallel int // documents read at once
	about    string
}

// timeBatch is the child of -gc: it reads every file of the list, the
// given number at once, as many times as -repeats and -budget allow, and
// prints the best run.
func timeBatch(list string, workers, parallel int) {
	raw, err := os.ReadFile(list)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	paths := strings.Fields(string(raw))
	cycles := []metrics.Sample{{Name: "/gc/cycles/total:gc-cycles"}}
	read := func() float64 {
		metrics.Read(cycles)
		return float64(cycles[0].Value.Uint64())
	}
	var r batchResult
	var spent time.Duration
	for r.Runs < *repeats && (r.Runs == 0 || spent < *budget) {
		cycles0 := read()
		cpu0 := processorTime()
		start := time.Now()
		pages, failed := readAll(paths, workers, parallel)
		took := time.Since(start)
		cpu := (processorTime() - cpu0).Seconds()
		cycles1 := read()
		if s := took.Seconds(); r.Runs == 0 || s < r.Seconds {
			r.Seconds, r.CPU, r.Cycles = s, cpu, cycles1-cycles0
		}
		r.Docs, r.Pages, r.Failed = len(paths), pages, failed
		spent += took
		r.Runs++
	}
	out, _ := json.Marshal(r)
	fmt.Println(string(out))
}

// readAll reads the files and makes their text, as the comparison does,
// and lets each document go when it is done.
func readAll(paths []string, workers, parallel int) (pages, failed int) {
	var mu sync.Mutex
	next := make(chan string)
	var wg sync.WaitGroup
	for range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range next {
				n, ok := readOne(path, workers)
				mu.Lock()
				pages += n
				if !ok {
					failed++
				}
				mu.Unlock()
			}
		}()
	}
	for _, p := range paths {
		next <- p
	}
	close(next)
	wg.Wait()
	return pages, failed
}

func readOne(path string, workers int) (pages int, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Workers: workers})
	if err != nil {
		return 0, false
	}
	_ = doc.Text()
	return len(doc.Parts), true
}

// measureGC measures each setting on each workload and writes the report.
func measureGC() error {
	docs, err := documents()
	if err != nil {
		return err
	}
	batch, err := tuningSet()
	if err != nil {
		return err
	}
	settings, err := parseSettings(*gcSettings)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "gofastpdf-gc-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	me, err := os.Executable()
	if err != nil {
		return err
	}

	n := runtime.NumCPU()
	long := workload{name: "One document at a time, one worker", short: "One worker", workers: 1, parallel: 1,
		about: "the documents of the comparison in the README, one after another, each read with one worker (Options.Workers 1)"}
	wide := workload{name: fmt.Sprintf("One document at a time, %d workers", n), short: fmt.Sprintf("%d workers", n), workers: n, parallel: 1,
		about: fmt.Sprintf("the same documents, each read with %d workers, one for each processor", n)}
	for _, d := range docs {
		long.paths = append(long.paths, d.path)
		long.pages += d.pages
		long.bytes += d.bytes
	}
	wide.paths, wide.pages, wide.bytes = long.paths, long.pages, long.bytes
	many := workload{name: fmt.Sprintf("%d documents at once, one worker each", n), short: fmt.Sprintf("%d documents at once", n), workers: 1, parallel: n,
		paths: batch.paths, pages: batch.pages, bytes: batch.bytes,
		about: fmt.Sprintf("the tuning set of the public corpus, %d documents at once, each read with one worker: a batch, damaged files included", n)}
	loads := []workload{long, wide, many}

	// results[w][s]
	results := make([][]batchResult, len(loads))
	for w, wl := range loads {
		list := filepath.Join(work, fmt.Sprintf("list-%d", w))
		if err := os.WriteFile(list, []byte(strings.Join(wl.paths, "\n")), 0o644); err != nil {
			return err
		}
		// The settings take turns, starting with a different one each
		// round, so that a slow moment of the machine does not fall on
		// one setting.
		each := make([][]batchResult, len(settings))
		for round := range max(*rounds, 1) {
			for i := range settings {
				s := (i + round) % len(settings)
				r := batchChild(settings[s], me, "-batch", list, "-workers", strconv.Itoa(wl.workers), "-parallel", strconv.Itoa(wl.parallel),
					"-repeats", strconv.Itoa(*repeats), "-budget", budget.String())
				each[s] = append(each[s], r)
				status := fmt.Sprintf("%8.1f ms %6d MB", r.Seconds*1000, r.Peak>>20)
				if r.Err != "" {
					status = r.Err
				}
				fmt.Fprintf(os.Stderr, "%-40s %-28s %s\n", wl.name, settingName(settings[s]), status)
			}
		}
		results[w] = make([]batchResult, len(settings))
		for s := range settings {
			results[w][s] = combine(each[s])
		}
	}

	report, summary := renderGC(loads, settings, results)
	if err := os.WriteFile(*gcOut, []byte(report), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "wrote", *gcOut)
	if *readmePath != "" {
		old, err := os.ReadFile(*readmePath)
		if err != nil {
			return err
		}
		updated, err := betweenMarkers(string(old), gcBeginMarker, gcEndMarker, summary)
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

// combine makes one result of the processes of a setting: the best
// time, with the processor time and the collections of that run, and
// the median of the memory, with the least and the most.
func combine(rs []batchResult) batchResult {
	var best batchResult
	var peaks []int64
	for i, r := range rs {
		if r.Err != "" {
			return r
		}
		if i == 0 || r.Seconds < best.Seconds {
			best = r
		}
		peaks = append(peaks, r.Peak)
	}
	slices.Sort(peaks)
	if len(peaks) > 0 {
		best.Peak, best.PeakLow, best.PeakHigh = peaks[len(peaks)/2], peaks[0], peaks[len(peaks)-1]
		if len(peaks)%2 == 0 {
			best.Peak = (peaks[len(peaks)/2-1] + peaks[len(peaks)/2]) / 2
		}
	}
	best.Processes = len(rs)
	return best
}

// batchChild runs a child with a setting of the collector, and with
// nothing of the collector's settings of this process: GOGC and
// GOMEMLIMIT are each set, to the setting's value or to the default.
func batchChild(set [][2]string, name string, args ...string) batchResult {
	env := map[string]string{"GOGC": "100", "GOMEMLIMIT": "off"}
	for _, kv := range set {
		env[kv[0]] = kv[1]
	}
	old := map[string]*string{}
	for k, v := range env {
		if prev, ok := os.LookupEnv(k); ok {
			old[k] = &prev
		} else {
			old[k] = nil
		}
		os.Setenv(k, v)
	}
	defer func() {
		for k, v := range old {
			if v == nil {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, *v)
			}
		}
	}()
	out, _, peak, err := pdftest.RunMeasured(*limit, name, args...)
	if err != nil {
		return batchResult{Err: "failed: " + clip(err.Error())}
	}
	line := strings.TrimSpace(out)
	if i := strings.LastIndexByte(line, '\n'); i >= 0 {
		line = line[i+1:]
	}
	var b batchResult
	if err := json.Unmarshal([]byte(line), &b); err != nil {
		return batchResult{Err: "no result"}
	}
	b.Peak = peak
	return b
}

// parseSettings reads "GOGC=100;GOGC=off GOMEMLIMIT=1GiB".
func parseSettings(s string) ([][][2]string, error) {
	var out [][][2]string
	for _, one := range strings.Split(s, ";") {
		var set [][2]string
		for _, kv := range strings.Fields(one) {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || (k != "GOGC" && k != "GOMEMLIMIT") || v == "" {
				return nil, fmt.Errorf("-gc-settings: %q is not GOGC=value or GOMEMLIMIT=value", kv)
			}
			set = append(set, [2]string{k, v})
		}
		if len(set) > 0 {
			out = append(out, set)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("-gc-settings: no setting")
	}
	return out, nil
}

func settingName(set [][2]string) string {
	var parts []string
	for _, kv := range set {
		parts = append(parts, kv[0]+"="+kv[1])
	}
	name := strings.Join(parts, " ")
	if name == "GOGC=100" {
		name += " (the default)"
	}
	return name
}

// tuningSet is every document of the tuning set of the public corpus
// that has been fetched.
func tuningSet() (workload, error) {
	var w workload
	raw, err := os.ReadFile(filepath.Join("testdata", "corpus-public.json"))
	if err != nil {
		return w, fmt.Errorf("run this from the root of the module: %v", err)
	}
	var manifest []struct {
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		Set   string `json:"set"`
		Pages int    `json:"pages"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return w, err
	}
	cache := corpusCache()
	for _, f := range manifest {
		if f.Set != "tune" {
			continue
		}
		path := filepath.Join(cache, filepath.FromSlash(f.Name))
		if _, err := os.Stat(path); err != nil {
			continue // not fetched
		}
		w.paths = append(w.paths, path)
		w.pages += f.Pages
		w.bytes += f.Size
	}
	if len(w.paths) == 0 {
		return w, fmt.Errorf("no documents of the public corpus in %s: run testdata/fetch.sh", cache)
	}
	return w, nil
}

// renderGC writes the report and the summary of the README.
func renderGC(loads []workload, settings [][][2]string, results [][]batchResult) (report, summary string) {
	ms := func(s float64) string { return number(s*1000) + " ms" }
	mb := func(b int64) string {
		if b <= 0 {
			return "-"
		}
		return thousands(float64(b>>20)) + " MB"
	}
	against := func(v, base float64) string {
		if base <= 0 || v <= 0 {
			return "-"
		}
		p := (v/base - 1) * 100
		if p > -0.5 && p < 0.5 {
			return "same"
		}
		return fmt.Sprintf("%+.0f%%", p)
	}

	var sum strings.Builder
	sum.WriteString("| Setting |")
	for _, wl := range loads {
		fmt.Fprintf(&sum, " %s: time | memory |", wl.short)
	}
	sum.WriteString("\n|---|")
	for range loads {
		sum.WriteString("---:|---:|")
	}
	sum.WriteString("\n")
	for s, set := range settings {
		label := "`" + strings.TrimSuffix(settingName(set), " (the default)") + "`"
		if strings.HasSuffix(settingName(set), " (the default)") {
			label += " (default)"
		}
		fmt.Fprintf(&sum, "| %s |", label)
		for w := range loads {
			r, base := results[w][s], results[w][0]
			if r.Err != "" {
				sum.WriteString(" failed | - |")
				continue
			}
			t, m := ms(r.Seconds), mb(r.Peak)
			if s > 0 {
				t += " (" + against(r.Seconds, base.Seconds) + ")"
				m += " (" + against(float64(r.Peak), float64(base.Peak)) + ")"
			}
			fmt.Fprintf(&sum, " %s | %s |", t, m)
		}
		sum.WriteString("\n")
	}
	fmt.Fprintf(&sum, "\nTime is the best run; memory is the most the process had resident, the median of %d processes. Measured on %s with %s, %s;\n", max(*rounds, 1), machine(), runtime.Version(), time.Now().Format("2006-01-02"))
	sum.WriteString("what each workload reads, the processor time and the number of collections are in [bench/gc.md](bench/gc.md).\n")

	var sb strings.Builder
	sb.WriteString("# The settings of the garbage collector\n\nWritten by `go run ./bench/compare -gc`. Do not edit: run it again.\n\n")
	sb.WriteString(`The library leaves the settings of Go's garbage collector alone: they are the program's, for the whole
process. A program that has memory to spare can set GOGC higher than 100 (the environment variable, or
debug.SetGCPercent), so that the collector runs less often, and spend less time collecting; or set a
memory limit (GOMEMLIMIT, or debug.SetMemoryLimit). This is what each setting costs and gives here.

`)
	sb.WriteString(sum.String())
	sb.WriteString("\n## What is read\n\n")
	for _, wl := range loads {
		fmt.Fprintf(&sb, "- **%s**: %s; %s documents, %s pages, %.1f MB.\n", wl.name, wl.about, thousands(float64(len(wl.paths))), thousands(float64(wl.pages)), float64(wl.bytes)/(1<<20))
	}
	sb.WriteString(`
Each setting is measured in processes of its own, started with GOGC and GOMEMLIMIT set and nothing else
changed; the settings take turns. A process reads every document of the workload and makes its text, as
the comparison does, and lets each document go when it is done; it does this again until its runs are
done. The time is the best run of all the processes of a setting.

## Each workload

- Processor time is that of the best run, on every processor together.
- Collections is the number of collections in the best run.
- Memory is the most that a process had resident over all its runs: the median of the processes, then the
  least and the most of them. It differs from one process to the next by which documents happen to be
  read at the same time, and by when the collector happens to run.
`)
	for w, wl := range loads {
		fmt.Fprintf(&sb, "\n### %s\n\n", wl.name)
		sb.WriteString("| Setting | Time | Pages per second | Processor time | Collections | Most memory | Least and most |\n|---|---:|---:|---:|---:|---:|---:|\n")
		for s, set := range settings {
			r := results[w][s]
			if r.Err != "" {
				fmt.Fprintf(&sb, "| %s | %s | | | | | |\n", settingName(set), r.Err)
				continue
			}
			cpu := "-"
			if r.CPU > 0 {
				cpu = ms(r.CPU)
			}
			fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %s | %s to %s |\n", settingName(set), ms(r.Seconds), thousands(float64(r.Pages)/r.Seconds),
				cpu, thousands(r.Cycles), mb(r.Peak), mb(r.PeakLow), mb(r.PeakHigh))
		}
		if r := results[w][0]; r.Failed > 0 {
			fmt.Fprintf(&sb, "\n%d of the %s documents end in an error (the corpus has damaged files on purpose); the time they take counts.\n", r.Failed, thousands(float64(r.Docs)))
		}
	}
	sb.WriteString(`
## What to make of it

- A higher GOGC lets the heap grow further between collections: GOGC=200 lets it grow to three times what
  is live, 400 to five times, where 100 lets it double. It gives the most where many goroutines make
  garbage at once: many workers, or many documents at once. With one worker, the collector already works
  on the other processors beside it, and a higher setting gives less.
- The memory is that of these documents. A larger document, or more of them at once, takes more, and the
  higher settings multiply that too; a memory limit (GOMEMLIMIT) keeps a high GOGC from going past what
  the machine has.
- A time that another job on the machine disturbed is too long, never too short; the best of the runs is
  the one disturbed least.
`)
	return sb.String(), sum.String()
}
