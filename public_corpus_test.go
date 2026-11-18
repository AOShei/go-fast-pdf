package gofastpdf_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// The public corpus (IMPLEMENTATION_PLAN.md section 4.5): several
// thousand PDFs from public test suites and collections, downloaded by
// testdata/fetch.sh into a cache outside the repository.
//
//   - testdata/corpus-public.json lists every file with its hash, its
//     labels and the set it belongs to (tune or held).
//   - testdata/baseline-public.json holds, per file, how the library did.
//
// Each file is read in a child process, so a crash or a hang is one
// result and not the end of the run. A file's result may improve and may
// not get worse. To rebuild the manifest and record new results after
// fetching or after a change to the library:
//
//	GOFASTPDF_UPDATE=1 go test -run TestPublicCorpus -timeout 2h .
//
// The test is skipped when the cache is empty, as on a fresh checkout,
// and with -short. Use -short with -race: the time limit per file assumes
// an uninstrumented build.

type publicFile struct {
	// Name is the path below the cache directory: source/file.pdf.
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Set is "tune" or "held". Heuristics are developed against the
	// tuning set only; the held-out set is scored and never inspected file
	// by file. The split is a fixed function of the hash, one third held.
	Set   string `json:"set"`
	Pages int    `json:"pages,omitempty"`
	// Producer is the family of the program that wrote the file, from its
	// document information. Script is the dominant script of its text.
	Producer string `json:"producer"`
	Script   string `json:"script"`
	// Flags: encrypted, linearized, tagged, damaged (the reference reader
	// reported syntax errors or failed).
	Flags []string `json:"flags,omitempty"`
	// Reference says whether pdftotext produced text to score against.
	Reference bool `json:"reference"`
}

// publicResult is the outcome for one file. Status is one of:
//
//	ok        extracted without error
//	error     the library returned an error
//	invalid   the library returned text that is not valid UTF-8
//	crash     panic or fatal error
//	timeout   no result within the time limit
type publicResult struct {
	Status string  `json:"status"`
	F1     float64 `json:"f1"`
	Order  float64 `json:"order"`
	Detail string  `json:"detail,omitempty"`
}

var statusRank = map[string]int{"timeout": 0, "crash": 0, "invalid": 1, "error": 2, "ok": 3}

func corpusCache() string {
	if dir := os.Getenv("GOFASTPDF_CACHE"); dir != "" {
		return dir
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "gofastpdf-corpus")
}

func refPath(cache, sum, ext string) string { return filepath.Join(cache, ".ref", sum+ext) }

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func TestPublicCorpus(t *testing.T) {
	cache := corpusCache()

	// Child process: extract one file, score it, hand back the result.
	if name := pdftest.ChildID(); name != "" {
		pdftest.ChildMain(func() (string, error) {
			sum, _ := fileSHA256(filepath.Join(cache, name))
			out, _ := json.Marshal(scorePublicFile(filepath.Join(cache, name), refPath(cache, sum, ".txt")))
			return string(out), nil
		})
	}

	entries, _ := filepath.Glob(filepath.Join(cache, "*", "*.pdf"))
	if len(entries) == 0 {
		t.Skipf("no public corpus in %s; run testdata/fetch.sh", cache)
	}
	if !pdftest.HavePdftotext() {
		t.Skip("pdftotext not installed")
	}
	if testing.Short() {
		t.Skip("the public corpus is skipped with -short")
	}
	update := pdftest.Updating()

	var manifest []publicFile
	if update {
		manifest = buildPublicManifest(t, cache)
		writeJSONLines(t, "testdata/corpus-public.json", manifest, func(f publicFile) string { return f.Name })
	} else {
		readJSON(t, "testdata/corpus-public.json", &manifest)
	}
	baseline := map[string]publicResult{}
	tolerance := 5e-4
	if !update {
		readJSON(t, "testdata/baseline-public.json", &baseline)
		recorded, _ := os.ReadFile("testdata/baseline-public.ref")
		if got, want := referenceVersion(), strings.TrimSpace(string(recorded)); got != want {
			tolerance = 0.02
			t.Logf("the baseline was scored against %q and this machine has %q: scores are compared with a tolerance of %.2f", want, got, tolerance)
		}
	}

	// Run every file that is present and is the version in the manifest.
	results := make([]*publicResult, len(manifest))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(runtime.NumCPU()/2, 1))
	for i, f := range manifest {
		path := filepath.Join(cache, f.Name)
		if !update {
			if sum, err := fileSHA256(path); err != nil || sum != f.SHA256 {
				continue
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ensureReference(cache, path, f.SHA256)
			results[i] = runPublicFile(t, f)
		}()
	}
	wg.Wait()

	ran, worse := 0, 0
	now := map[string]publicResult{}
	for i, f := range manifest {
		r := results[i]
		if r == nil {
			continue
		}
		ran++
		now[f.Name] = *r
		old, known := baseline[f.Name]
		if update || !known {
			continue
		}
		if statusRank[r.Status] < statusRank[old.Status] {
			worse++
			t.Errorf("%s: was %s, now %s (%s)", f.Name, old.Status, r.Status, r.Detail)
		} else if r.Status == "ok" && old.Status == "ok" && (r.F1 < old.F1-tolerance || r.Order < old.Order-tolerance) {
			worse++
			t.Errorf("%s: F1 %.4f (was %.4f), order %.4f (was %.4f)", f.Name, r.F1, old.F1, r.Order, old.Order)
		}
	}
	summary := summarizePublic(manifest, now)
	t.Logf("%d of %d files present, %d worse than the baseline\n%s", ran, len(manifest), worse, summary)

	if update {
		var names []string
		for name := range now {
			names = append(names, name)
		}
		sort.Strings(names)
		type row struct {
			Name string `json:"name"`
			publicResult
		}
		rows := make([]row, len(names))
		for i, name := range names {
			rows[i] = row{name, now[name]}
		}
		// Stored as a map for lookup, written one file per line for diffs.
		var sb strings.Builder
		sb.WriteString("{\n")
		for i, r := range rows {
			line, _ := json.Marshal(r.publicResult)
			fmt.Fprintf(&sb, "  %q: %s", r.Name, line)
			if i < len(rows)-1 {
				sb.WriteByte(',')
			}
			sb.WriteByte('\n')
		}
		sb.WriteString("}\n")
		if err := os.WriteFile("testdata/baseline-public.json", []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("testdata/baseline-public.txt", []byte(summary), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("testdata/baseline-public.ref", []byte(referenceVersion()+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runPublicFile(t *testing.T, f publicFile) *publicResult {
	out, err := pdftest.Spawn(t, f.Name, 30*time.Second)
	if err != nil {
		status := "crash"
		if strings.Contains(err.Error(), "hang") {
			status = "timeout"
		}
		return &publicResult{Status: status, Detail: clipString(err.Error(), 120)}
	}
	var r publicResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return &publicResult{Status: "crash", Detail: "no result from the child process"}
	}
	return &r
}

// scorePublicFile runs in the child process.
func scorePublicFile(path, refFile string) publicResult {
	got, err := extractFile(path, extractOpts{})
	if err != nil {
		if strings.Contains(err.Error(), "UTF-8") {
			return publicResult{Status: "invalid", Detail: clipString(err.Error(), 120)}
		}
		return publicResult{Status: "error", Detail: clipString(err.Error(), 120)}
	}
	r := publicResult{Status: "ok"}
	if ref, err := os.ReadFile(refFile); err == nil {
		want := strings.Split(string(ref), "\f")
		s := pdftest.ComparePages(got.pageTexts(), want)
		r.F1, r.Order = round4(s.F1), round4(s.Order)
	}
	return r
}

func clipString(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n]
	}
	return s
}

// buildPublicManifest lists the cache, and for each file records its
// hash, runs the reference reader once (results are kept in the cache)
// and derives the labels.
func buildPublicManifest(t *testing.T, cache string) []publicFile {
	var paths []string
	filepath.WalkDir(cache, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".pdf") {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	if err := os.MkdirAll(filepath.Join(cache, ".ref"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := make([]publicFile, len(paths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i, path := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = labelPublicFile(cache, path)
		}()
	}
	wg.Wait()
	return out
}

// runTool runs a reference tool with a time limit. Its output is capped:
// on a damaged file a tool can print error messages without end, and
// 1,650 files are read in parallel.
func runTool(timeout time.Duration, name string, args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command(name, args...)
	so, se := &cappedBuffer{limit: 64 << 20}, &cappedBuffer{limit: 1 << 20}
	cmd.Stdout, cmd.Stderr = so, se
	if err := cmd.Start(); err != nil {
		return "", "", err
	}
	timer := time.AfterFunc(timeout, func() { cmd.Process.Kill() })
	err = cmd.Wait()
	timer.Stop()
	if err == nil && so.full {
		err = fmt.Errorf("%s: more than %d MB of output", name, so.limit>>20)
	}
	return so.sb.String(), se.sb.String(), err
}

// cappedBuffer keeps the first limit bytes written to it and discards
// the rest.
type cappedBuffer struct {
	sb    strings.Builder
	limit int
	full  bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.sb.Len(); room >= len(p) {
		c.sb.Write(p)
	} else {
		c.sb.Write(p[:max(room, 0)])
		c.full = true
	}
	return len(p), nil
}

// ensureReference runs the reference reader on a file once and keeps its
// text and document information in the cache.
func ensureReference(cache, path, sum string) {
	txt, info := refPath(cache, sum, ".txt"), refPath(cache, sum, ".info")
	if _, err := os.Stat(info); err == nil {
		return
	}
	os.MkdirAll(filepath.Dir(info), 0o755)
	text, textErr, err1 := runTool(2*time.Minute, "pdftotext", "-enc", "UTF-8", path, "-")
	meta, metaErr, err2 := runTool(time.Minute, "pdfinfo", path)
	if err1 == nil {
		os.WriteFile(txt, []byte(text), 0o644)
	}
	status := "clean"
	if err1 != nil || err2 != nil || strings.Contains(textErr+metaErr, "Syntax Error") {
		status = "damaged"
	}
	os.WriteFile(info, []byte("Status: "+status+"\n"+meta), 0o644)
}

// referenceVersion identifies the reference reader, e.g. "pdftotext
// version 25.10.0". Scores recorded against one version are compared
// loosely against another, because the reference text itself differs.
func referenceVersion() string {
	_, stderr, _ := runTool(10*time.Second, "pdftotext", "-v")
	line, _, _ := strings.Cut(strings.TrimSpace(stderr), "\n")
	return strings.TrimSpace(line)
}

func labelPublicFile(cache, path string) publicFile {
	rel, _ := filepath.Rel(cache, path)
	f := publicFile{Name: filepath.ToSlash(rel)}
	data, err := os.ReadFile(path)
	if err != nil {
		return f
	}
	sum := sha256.Sum256(data)
	f.SHA256, f.Size = hex.EncodeToString(sum[:]), int64(len(data))
	f.Set = "tune"
	if binary.BigEndian.Uint32(sum[:4])%3 == 0 {
		f.Set = "held"
	}

	ensureReference(cache, path, f.SHA256)
	txt, info := refPath(cache, f.SHA256, ".txt"), refPath(cache, f.SHA256, ".info")
	meta, _ := os.ReadFile(info)
	fields := map[string]string{}
	for _, line := range strings.Split(string(meta), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fields[k] = strings.TrimSpace(v)
		}
	}
	fmt.Sscan(fields["Pages"], &f.Pages)
	f.Producer = producerFamily(fields["Producer"] + " " + fields["Creator"])
	if strings.HasPrefix(fields["Encrypted"], "yes") {
		f.Flags = append(f.Flags, "encrypted")
	}
	if fields["Optimized"] == "yes" {
		f.Flags = append(f.Flags, "linearized")
	}
	if fields["Tagged"] == "yes" {
		f.Flags = append(f.Flags, "tagged")
	}
	if fields["Status"] == "damaged" {
		f.Flags = append(f.Flags, "damaged")
	}
	if text, err := os.ReadFile(txt); err == nil {
		f.Reference = true
		f.Script = dominantScript(string(text))
	} else {
		f.Script = "unknown"
	}
	return f
}

// producerFamily maps the Producer and Creator strings to a coarse
// family. The order matters: the first match wins.
func producerFamily(s string) string {
	s = strings.ToLower(s)
	families := []struct {
		family string
		marks  []string
	}{
		{"scanner", []string{"scan", "capture", "abbyy", "omnipage", "tesseract", "readiris", "ocr", "xerox", "ricoh", "canon", "kofax"}},
		{"tex", []string{"tex", "dvips", "dvipdf"}},
		{"office", []string{"microsoft", "word", "excel", "powerpoint", "openoffice", "libreoffice", "wordperfect", "pages", "keynote"}},
		{"browser", []string{"chrom", "skia", "mozilla", "firefox", "webkit", "wkhtmltopdf", "prince", "weasyprint"}},
		{"dtp", []string{"indesign", "quark", "pagemaker", "framemaker", "illustrator", "photoshop", "coreldraw", "scribus", "publisher"}},
		{"library", []string{"itext", "fpdf", "tcpdf", "reportlab", "pdflib", "cairo", "qt ", "crystal", "jasper", "oracle", "pdfkit", "pdfsharp", "apache fop", "bfo", "pdf-lib", "pdfbox", "mupdf", "qpdf", "pdfium", "poppler", "prawn", "big faceless"}},
		{"print-driver", []string{"distiller", "pdfmaker", "ghostscript", "pscript", "acrobat", "pdfwriter", "quartz", "mac os x", "cups", "pdfcreator", "cutepdf", "primopdf", "jaws", "amyuni", "bullzip"}},
	}
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	for _, f := range families {
		for _, m := range f.marks {
			if strings.Contains(s, m) {
				return f.family
			}
		}
	}
	return "other"
}

// dominantScript returns the script of most letters in text.
func dominantScript(text string) string {
	counts := map[string]int{}
	n := 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		n++
		switch {
		case unicode.Is(unicode.Latin, r):
			counts["latin"]++
		case unicode.Is(unicode.Cyrillic, r):
			counts["cyrillic"]++
		case unicode.Is(unicode.Greek, r):
			counts["greek"]++
		case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
			counts["cjk"]++
		case unicode.Is(unicode.Arabic, r), unicode.Is(unicode.Hebrew, r):
			counts["rtl"]++
		default:
			counts["other"]++
		}
		if n >= 20000 {
			break
		}
	}
	if n < 20 {
		return "none"
	}
	best, bestN := "other", 0
	for k, c := range counts {
		if c > bestN || (c == bestN && k < best) {
			best, bestN = k, c
		}
	}
	return best
}

// writeJSONLines writes a JSON array with one element per line, sorted
// by key, so that a change to the corpus shows as a line-by-line diff.
func writeJSONLines[T any](t *testing.T, path string, items []T, key func(T) string) {
	t.Helper()
	sort.Slice(items, func(i, j int) bool { return key(items[i]) < key(items[j]) })
	var sb strings.Builder
	sb.WriteString("[\n")
	for i, it := range items {
		line, err := json.Marshal(it)
		if err != nil {
			t.Fatal(err)
		}
		sb.WriteString("  ")
		sb.Write(line)
		if i < len(items)-1 {
			sb.WriteByte(',')
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("]\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// summarizePublic tabulates the results by set and by each label. "read"
// is the share of files with status ok. Mean F1 and order are over the
// files that were read and whose reference contains text.
func summarizePublic(manifest []publicFile, results map[string]publicResult) string {
	type agg struct {
		files, ok, scored int
		f1, order         float64
		status            map[string]int
	}
	groups := map[string]*agg{}
	var order []string
	add := func(group string, f publicFile, r publicResult) {
		a := groups[group]
		if a == nil {
			a = &agg{status: map[string]int{}}
			groups[group] = a
			order = append(order, group)
		}
		a.files++
		a.status[r.Status]++
		if r.Status == "ok" {
			a.ok++
			// Files whose reference has no text are left out of the means:
			// two empty texts score 1 and would flatter the average.
			if f.Reference && f.Script != "none" {
				a.scored++
				a.f1 += r.F1
				a.order += r.Order
			}
		}
	}
	for _, f := range manifest {
		r, ok := results[f.Name]
		if !ok {
			continue
		}
		add("all", f, r)
		add("set="+f.Set, f, r)
		add("source="+strings.SplitN(f.Name, "/", 2)[0], f, r)
		add("producer="+f.Producer, f, r)
		add("script="+f.Script, f, r)
		if len(f.Flags) == 0 {
			add("flag=none", f, r)
		}
		for _, flag := range f.Flags {
			add("flag="+flag, f, r)
		}
	}
	sort.Strings(order)
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-24s %6s %6s %8s %8s   %s\n", "group", "files", "read", "mean F1", "order", "not read")
	for _, g := range order {
		a := groups[g]
		f1, ord := "-", "-"
		if a.scored > 0 {
			f1 = fmt.Sprintf("%.4f", a.f1/float64(a.scored))
			ord = fmt.Sprintf("%.4f", a.order/float64(a.scored))
		}
		var bad []string
		for _, s := range []string{"error", "invalid", "crash", "timeout"} {
			if a.status[s] > 0 {
				bad = append(bad, fmt.Sprintf("%s %d", s, a.status[s]))
			}
		}
		note := ""
		if a.files < 10 {
			note = "  (under-represented)"
		}
		fmt.Fprintf(&sb, "%-24s %6d %5.1f%% %8s %8s   %s%s\n", g, a.files, 100*float64(a.ok)/float64(a.files), f1, ord, strings.Join(bad, ", "), note)
	}
	return sb.String()
}
