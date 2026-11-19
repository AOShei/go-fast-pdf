package gofastpdf_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
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
//	locked    the file is encrypted and needs a password
//	error     the library returned any other error
//	invalid   the library returned text that is not valid UTF-8
//	crash     panic or fatal error
//	timeout   no result within the time limit
type publicResult struct {
	Status string  `json:"status"`
	F1     float64 `json:"f1"`
	Order  float64 `json:"order"`
	Detail string  `json:"detail,omitempty"`
	// Ref identifies the words of the reference text that the scores
	// were measured against. Another version of pdftotext reads some
	// files differently; a score against other words says nothing about
	// the library, and is not compared.
	Ref string `json:"ref,omitempty"`
}

var statusRank = map[string]int{"timeout": 0, "crash": 0, "invalid": 1, "error": 2, "locked": 2, "ok": 3}

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
	const tolerance = 5e-4
	if !update {
		readJSON(t, "testdata/baseline-public.json", &baseline)
		recorded, _ := os.ReadFile("testdata/baseline-public.ref")
		if got, want := referenceVersion(), strings.TrimSpace(string(recorded)); got != want {
			t.Logf("the baseline was scored against %q and this machine has %q: a file that the two read differently is not compared", want, got)
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

	ran, worse, otherWords := 0, 0, 0
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
		} else if r.Status == "ok" && old.Status == "ok" && r.Ref != old.Ref {
			otherWords++
		} else if r.Status == "ok" && old.Status == "ok" && (r.F1 < old.F1-tolerance || r.Order < old.Order-tolerance) {
			worse++
			t.Errorf("%s: F1 %.4f (was %.4f), order %.4f (was %.4f)", f.Name, r.F1, old.F1, r.Order, old.Order)
		}
	}
	summary := summarizePublic(manifest, now)
	t.Logf("%d of %d files present, %d worse than the baseline\n%s", ran, len(manifest), worse, summary)
	if otherWords > 0 {
		t.Logf("%d files have other words in their reference text than when the baseline was recorded; their scores are not compared", otherWords)
		// The comparison must not become empty without anybody noticing.
		if otherWords > ran/5 {
			t.Errorf("%d of %d files have another reference text than the baseline: record the baseline with this version of pdftotext", otherWords, ran)
		}
	}

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
	if err == nil {
		// The same document for any number of workers.
		many, err := extractFile(path, extractOpts{Workers: 8})
		if err != nil {
			return publicResult{Status: "invalid", Detail: clipString("with 8 workers: "+err.Error(), 120)}
		}
		if diff := sameDocument(got.Doc, many.Doc); diff != "" {
			return publicResult{Status: "invalid", Detail: clipString("1 worker and 8 workers: "+diff, 120)}
		}
		// And with the checks of DeepHidden: a document that keeps the
		// contract, the same for any number of workers.
		deep, err := extractFile(path, extractOpts{Deep: true})
		if err != nil {
			return publicResult{Status: "invalid", Detail: clipString("with DeepHidden: "+err.Error(), 120)}
		}
		deepMany, err := extractFile(path, extractOpts{Deep: true, Workers: 8})
		if err != nil {
			return publicResult{Status: "invalid", Detail: clipString("with DeepHidden and 8 workers: "+err.Error(), 120)}
		}
		if diff := sameDocument(deep.Doc, deepMany.Doc); diff != "" {
			return publicResult{Status: "invalid", Detail: clipString("with DeepHidden, 1 worker and 8 workers: "+diff, 120)}
		}
		if diff := deepAgainstDefault(got.Doc, deep.Doc); diff != "" {
			return publicResult{Status: "invalid", Detail: clipString("with DeepHidden: "+diff, 160)}
		}
		// The parts handed out one by one are those of the document, and
		// the lines of every page are those of its blocks.
		if diff := handleAgainstDocument(path, got.Doc); diff != "" {
			return publicResult{Status: "invalid", Detail: clipString("the file handle: "+diff, 160)}
		}
	}
	if err != nil {
		if errors.Is(err, errContract) {
			return publicResult{Status: "invalid", Detail: clipString(err.Error(), 120)}
		}
		if errors.Is(err, gofastcore.ErrPasswordRequired) {
			return publicResult{Status: "locked"}
		}
		return publicResult{Status: "error", Detail: clipString(err.Error(), 120)}
	}
	r := publicResult{Status: "ok"}
	if ref, err := os.ReadFile(refFile); err == nil {
		want := strings.Split(string(ref), "\f")
		s := pdftest.ComparePages(got.pageTexts(), want)
		r.F1, r.Order = round4(s.F1), round4(s.Order)
		r.Ref = wordsHash(want)
	}
	return r
}

// wordsHash identifies the words of a text, page by page, as the scorer
// reads them.
func wordsHash(pages []string) string {
	h := sha256.New()
	for _, page := range pages {
		for _, w := range pdftest.Words(page) {
			h.Write([]byte(w))
			h.Write([]byte{0})
		}
		h.Write([]byte{'\f'})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// deepAgainstDefault checks what the checks of DeepHidden may change of
// a document: they flag text, and take back the flag of text that was
// taken for white on white. They add no text and lose none.
//
// Letters are compared, page by page, since blocks cannot be: hidden
// text is laid out apart from the rest, and of text that a page prints
// twice the copy that is kept is the one in view, which may be drawn
// at another time than the one kept without the checks.
//
//   - A letter in view with the checks is in view without them, or
//     flagged without them for its contrast alone.
//   - A letter of the page without the checks is on the page with them.
func deepAgainstDefault(plain, deep *gofastcore.Document) string {
	if len(plain.Parts) != len(deep.Parts) {
		return fmt.Sprintf("%d parts, and %d without", len(deep.Parts), len(plain.Parts))
	}
	letters := func(p *gofastcore.Part, keep func(gofastcore.Hidden) bool) map[rune]int {
		m := map[rune]int{}
		for j := range p.Blocks {
			if b := &p.Blocks[j]; keep(b.Hidden) {
				for _, r := range b.Text {
					if unicode.IsLetter(r) || unicode.IsDigit(r) {
						m[r]++
					}
				}
			}
		}
		return m
	}
	any := func(gofastcore.Hidden) bool { return true }
	for i := range plain.Parts {
		p, d := &plain.Parts[i], &deep.Parts[i]
		was := letters(p, func(h gofastcore.Hidden) bool { return h&^gofastcore.HiddenLowContrast == 0 })
		for r, n := range letters(d, func(h gofastcore.Hidden) bool { return h == 0 }) {
			if n > was[r] {
				return fmt.Sprintf("page %d: %q is in view %d times, and %d times without the checks", p.Number, r, n, was[r])
			}
		}
		all := letters(d, any)
		for r, n := range letters(p, any) {
			if n > all[r] {
				return fmt.Sprintf("page %d: %q is on the page %d times, and %d times without the checks", p.Number, r, all[r], n)
			}
		}
	}
	return ""
}

// handleAgainstDocument reads a file through the native API and
// compares what it hands out with the document that Extract gave: the
// parts of the iterator, and for every page its lines.
func handleAgainstDocument(path string, doc *gofastcore.Document) string {
	f, err := gofastpdf.Open(path, &gofastcore.Options{Workers: 8, Images: gofastcore.ImagesNone})
	if err != nil {
		return "Open: " + err.Error()
	}
	defer f.Close()
	i := 0
	for part, err := range f.Parts(context.Background()) {
		if err != nil {
			return "Parts: " + err.Error()
		}
		if i >= len(doc.Parts) || !reflect.DeepEqual(part, doc.Parts[i]) {
			return fmt.Sprintf("part %d of the iterator is not that of the document", i+1)
		}
		i++
	}
	if i != len(doc.Parts) {
		return fmt.Sprintf("%d parts from the iterator and %d in the document", i, len(doc.Parts))
	}
	for i := range doc.Parts {
		part := &doc.Parts[i]
		if part.Kind == gofastcore.PartStored {
			continue // not a page
		}
		page, err := f.Page(part.Number)
		if err != nil {
			return err.Error()
		}
		lines, err := page.Lines()
		if err != nil {
			return err.Error()
		}
		if diff := linesAgainstPart(lines, part); diff != "" {
			return fmt.Sprintf("page %d: %s", part.Number, diff)
		}
	}
	return ""
}

// linesAgainstPart checks the lines of a page against its part: each
// line is in a text block, in the order of the blocks, hidden as its
// block is; its words are its text; and the letters of the lines of a
// block are those of the block.
func linesAgainstPart(lines []gofastpdf.Line, part *gofastcore.Part) string {
	letters := func(s string) string {
		var sb strings.Builder
		for _, r := range s {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				sb.WriteRune(r)
			}
		}
		return sb.String()
	}
	perBlock := map[int]string{}
	last := -1
	for _, l := range lines {
		if k := part.Blocks[min(max(l.Block, 0), len(part.Blocks)-1)].Kind; l.Block < last || l.Block >= len(part.Blocks) || k != gofastcore.KindParagraph && !k.Furniture() {
			return fmt.Sprintf("a line of block %d after one of block %d, of %d blocks", l.Block, last, len(part.Blocks))
		}
		last = l.Block
		if l.Hidden != part.Blocks[l.Block].Hidden {
			return fmt.Sprintf("a line is %q and its block %q", l.Hidden, part.Blocks[l.Block].Hidden)
		}
		perBlock[l.Block] += letters(l.Text)
		var words []string
		for _, w := range l.Words {
			words = append(words, w.Text)
			for _, v := range [...]float64{w.BBox.X0, w.BBox.Y0, w.BBox.X1, w.BBox.Y1} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return fmt.Sprintf("the word %.20q has a box that is not a number", w.Text)
				}
			}
		}
		if strings.Join(strings.Fields(strings.Join(words, " ")), " ") != strings.Join(strings.Fields(l.Text), " ") {
			return fmt.Sprintf("the line %.40q has the words %.60q", l.Text, words)
		}
	}
	for i := range part.Blocks {
		b := &part.Blocks[i]
		if _, field := b.Attrs[gofastcore.AttrField]; field {
			continue // the value of a form field is not set in lines of the page
		}
		if (b.Kind == gofastcore.KindParagraph || b.Kind.Furniture()) && perBlock[i] != letters(b.Text) {
			return fmt.Sprintf("block %d is %.30q and its lines have the letters %.30q", i, b.Text, perBlock[i])
		}
	}
	return ""
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
		// Not the reference texts, and not the files that are there for
		// the soak test alone: those are read and never scored.
		if err == nil && d.IsDir() && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), soakOnly)) {
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

// Bounds on the cache of reference text. The largest reference in the
// corpus is 2 MB; a tool that produces more than refMaxBytes for one file
// is reading a damaged or hostile one, and its output is not kept. The
// cache as a whole stops growing at refCacheMaxBytes.
const (
	refMaxBytes      = 8 << 20
	refCacheMaxBytes = 512 << 20
)

// refCacheBytes is the size of the reference cache, counted once from
// the directory and then kept up to date.
var (
	refCacheBytes atomic.Int64
	refCacheOnce  sync.Once
)

// ensureReference runs the reference reader on a file once and keeps its
// text and document information in the cache. The tools are run
// contained (pdftest.Run).
func ensureReference(cache, path, sum string) {
	txt, info := refPath(cache, sum, ".txt"), refPath(cache, sum, ".info")
	if _, err := os.Stat(info); err == nil {
		return
	}
	dir := filepath.Dir(info)
	os.MkdirAll(dir, 0o755)
	refCacheOnce.Do(func() {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if fi, err := e.Info(); err == nil {
				refCacheBytes.Add(fi.Size())
			}
		}
	})
	text, textErr, err1 := pdftest.RunCapped(2*time.Minute, refMaxBytes, "pdftotext", "-enc", "UTF-8", path, "-")
	meta, metaErr, err2 := pdftest.Run(time.Minute, "pdfinfo", path)
	if len(meta) > 64<<10 {
		meta = meta[:64<<10]
	}
	if err1 == nil && refCacheBytes.Add(int64(len(text))) <= refCacheMaxBytes {
		os.WriteFile(txt, []byte(text), 0o644)
	}
	status := "clean"
	if err1 != nil || err2 != nil || strings.Contains(textErr+metaErr, "Syntax Error") {
		status = "damaged"
	}
	refCacheBytes.Add(int64(len(meta)) + 32)
	os.WriteFile(info, []byte("Status: "+status+"\n"+meta), 0o644)
}

// referenceVersion identifies the reference reader, e.g. "pdftotext
// version 25.10.0". Scores recorded against one version are compared
// loosely against another, because the reference text itself differs.
func referenceVersion() string {
	_, stderr, _ := pdftest.Run(10*time.Second, "pdftotext", "-v")
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
		for _, s := range []string{"locked", "error", "invalid", "crash", "timeout"} {
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
