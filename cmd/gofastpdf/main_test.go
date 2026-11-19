package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

const fixtures = "../../testdata/fixtures"

// command runs the command and returns what it wrote and its status.
func command(t *testing.T, args ...string) (stdout, stderr string, status int) {
	t.Helper()
	var out, errs bytes.Buffer
	status = run(context.Background(), args, &out, &errs)
	return out.String(), errs.String(), status
}

// document decodes one JSON document and checks it against the
// contract of the shared model.
func document(t *testing.T, data string) *gofastcore.Document {
	t.Helper()
	var doc gofastcore.Document
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		t.Fatalf("the output is not a document: %v\n%.200s", err, data)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the document breaks the contract: %v", err)
	}
	return &doc
}

// write puts a synthetic document into a temporary file.
func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestEveryFixture: every file of the fixture corpus gives a document
// that keeps the contract, or a message and status 1 when it needs a
// password; nothing but the document goes to standard output.
func TestEveryFixture(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join(fixtures, "*.pdf"))
	if len(paths) < 30 {
		t.Fatalf("%d fixtures found", len(paths))
	}
	read, locked := 0, 0
	for _, path := range paths {
		stdout, stderr, status := command(t, path)
		switch status {
		case 0:
			doc := document(t, stdout)
			if doc.Source.Name != filepath.Base(path) || len(doc.Parts) == 0 {
				t.Errorf("%s: source %+v, %d parts", path, doc.Source, len(doc.Parts))
			}
			if !strings.HasPrefix(stdout, "{\n  \"") {
				t.Errorf("%s: one document is not indented: %.40q", path, stdout)
			}
			read++
		case 1:
			if stdout != "" || !strings.Contains(stderr, filepath.Base(path)) || !strings.Contains(stderr, "password") {
				t.Errorf("%s: status 1 with output %.60q and message %q", path, stdout, stderr)
			}
			locked++
		default:
			t.Errorf("%s: status %d: %s", path, status, stderr)
		}
	}
	if read < 30 || locked == 0 {
		t.Errorf("%d fixtures read and %d that need a password", read, locked)
	}
}

func TestFormats(t *testing.T) {
	english := filepath.Join(fixtures, "english.pdf")
	text, _, status := command(t, "-format", "text", english)
	if status != 0 || !strings.HasPrefix(text, "Extracting text from documents\n") {
		t.Errorf("text: status %d, %.80q", status, text)
	}
	md, _, status := command(t, "--format=markdown", english)
	if status != 0 || !strings.Contains(md, "Extracting text from documents") || strings.HasPrefix(md, "{") {
		t.Errorf("markdown: status %d, %.80q", status, md)
	}
	// The double dash and the single one are the same flag.
	if again, _, _ := command(t, "--format", "text", english); again != text {
		t.Error("--format and -format differ")
	}
}

func TestSeveralInputs(t *testing.T) {
	english, multi := filepath.Join(fixtures, "english.pdf"), filepath.Join(fixtures, "multilingual.pdf")
	missing := filepath.Join(fixtures, "no-such-file.pdf")

	// One document per line, in the order given; a file that fails is
	// reported, the others are written, and the status says so.
	stdout, stderr, status := command(t, english, missing, multi)
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if status != 1 || len(lines) != 2 || !strings.Contains(stderr, "no-such-file.pdf") {
		t.Fatalf("status %d, %d lines, stderr %q", status, len(lines), stderr)
	}
	for i, want := range []string{"english.pdf", "multilingual.pdf"} {
		if doc := document(t, lines[i]); doc.Source.Name != want {
			t.Errorf("line %d is %q, want %q", i, doc.Source.Name, want)
		}
	}

	// A directory stands for the PDF files in it and below it, in order.
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	for _, name := range []string{"b.pdf", "a.PDF", "sub/c.pdf", "notes.txt"} {
		os.WriteFile(filepath.Join(dir, name), pdftest.OnePage(pdftest.TextPage("page of "+name)), 0o644)
	}
	stdout, stderr, status = command(t, dir)
	lines = strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if status != 0 || len(lines) != 3 || stderr != "" {
		t.Fatalf("a directory: status %d, %d lines, stderr %q", status, len(lines), stderr)
	}
	for i, want := range []string{"a.PDF", "b.pdf", "c.pdf"} {
		if doc := document(t, lines[i]); doc.Source.Name != want {
			t.Errorf("line %d is %q, want %q", i, doc.Source.Name, want)
		}
	}

	// Text and Markdown of several inputs say which file each is.
	text, _, _ := command(t, "-format", "text", english, multi)
	if !strings.HasPrefix(text, "==> "+english+" <==\n") || !strings.Contains(text, "\n==> "+multi+" <==\n") {
		t.Errorf("text of two files: %.80q", text)
	}
	md, _, _ := command(t, "-format", "markdown", english, multi)
	if !strings.HasPrefix(md, "<!-- "+english+" -->\n") || !strings.Contains(md, "<!-- "+multi+" -->") {
		t.Errorf("markdown of two files: %.80q", md)
	}
}

func TestPasswordAndPages(t *testing.T) {
	locked := filepath.Join(fixtures, "enc-aes-256-userpw.pdf")
	if _, stderr, status := command(t, locked); status != 1 || !strings.Contains(stderr, "password") {
		t.Errorf("without the password: status %d, %q", status, stderr)
	}
	if _, stderr, status := command(t, "-password", "wrong", locked); status != 1 || !strings.Contains(stderr, "password") {
		t.Errorf("with a wrong password: status %d, %q", status, stderr)
	}
	stdout, stderr, status := command(t, "--password", "user", locked)
	if status != 0 {
		t.Fatalf("with the password: status %d, %q", status, stderr)
	}
	if doc := document(t, stdout); !doc.Metadata.Encrypted || len(doc.Parts) == 0 {
		t.Errorf("encrypted %v, %d parts", doc.Metadata.Encrypted, len(doc.Parts))
	}

	five := write(t, "five.pdf", pdftest.Doc(pdftest.DocOptions{},
		pdftest.PageSpec{Content: pdftest.TextPage("one")}, pdftest.PageSpec{Content: pdftest.TextPage("two")},
		pdftest.PageSpec{Content: pdftest.TextPage("three")}, pdftest.PageSpec{Content: pdftest.TextPage("four")},
		pdftest.PageSpec{Content: pdftest.TextPage("five")}).Bytes())
	for pages, want := range map[string]string{"3": "three", "2-4": "two three four", "4-": "four five", "-2": "one two", "1-9": "one two three four five"} {
		text, stderr, status := command(t, "-format", "text", "-pages", pages, five)
		if got := strings.Join(strings.Fields(text), " "); status != 0 || got != want {
			t.Errorf("-pages %s: status %d, %q, want %q (%s)", pages, status, got, want, stderr)
		}
	}
	// The numbers of the parts are those of the pages in the file.
	stdout, _, _ = command(t, "-pages", "4-", five)
	if doc := document(t, stdout); len(doc.Parts) != 2 || doc.Parts[0].Number != 4 {
		t.Errorf("-pages 4-: %d parts, the first is page %d", len(doc.Parts), doc.Parts[0].Number)
	}
	if _, stderr, status := command(t, "-pages", "7-", five); status != 1 || !strings.Contains(stderr, "five.pdf") {
		t.Errorf("pages the file does not have: status %d, %q", status, stderr)
	}
}

// hiddenPage has a visible line, a line in render mode 3 and white
// text on a white box over a dark page, which only the deep checks
// find.
const hiddenPage = "BT /F1 12 Tf 72 720 Td (in plain sight) Tj ET\n" +
	"BT /F1 12 Tf 3 Tr 72 600 Td (not painted) Tj ET\n" +
	"0.1 g 0 300 612 200 re f 1 g 60 390 300 30 re f BT /F1 12 Tf 0 Tr 72 400 Td (white on white) Tj ET\n"

func TestHiddenText(t *testing.T) {
	path := write(t, "hidden.pdf", pdftest.OnePage(hiddenPage))
	reasons := func(args ...string) map[string]string {
		t.Helper()
		stdout, stderr, status := command(t, append(args, path)...)
		if status != 0 {
			t.Fatalf("%v: status %d, %s", args, status, stderr)
		}
		got := map[string]string{}
		for _, b := range document(t, stdout).Parts[0].Blocks {
			got[b.Text] = b.Hidden.String()
		}
		return got
	}
	// JSON has everything, flagged; the deep checks find more.
	if got := reasons(); got["in plain sight"] != "" || got["not painted"] != "invisible" || got["white on white"] != "" {
		t.Errorf("by default: %v", got)
	}
	if got := reasons("-deep-hidden"); got["not painted"] != "invisible" || got["white on white"] != "low_contrast" {
		t.Errorf("with -deep-hidden: %v", got)
	}
	// -exclude-hidden changes nothing in JSON.
	if got := reasons("-exclude-hidden"); len(got) != 3 {
		t.Errorf("JSON with -exclude-hidden: %v", got)
	}

	// The warnings of a file that was read are named on standard error,
	// unless -quiet.
	_, stderr, _ := command(t, path)
	if !strings.Contains(stderr, "hidden.pdf: text.hidden") {
		t.Errorf("the report: %q", stderr)
	}
	if _, stderr, _ := command(t, "-quiet", path); stderr != "" {
		t.Errorf("with -quiet: %q", stderr)
	}

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{}, "in plain sight not painted white on white"},
		{[]string{"-exclude-hidden"}, "in plain sight white on white"},
		{[]string{"-exclude-hidden", "-deep-hidden"}, "in plain sight"},
		{[]string{"--exclude-hidden=low_contrast", "--deep-hidden"}, "in plain sight not painted"},
		{[]string{"-exclude-hidden=invisible,low_contrast", "-deep-hidden"}, "in plain sight"},
		{[]string{"-exclude-hidden=tiny+off_page", "-deep-hidden"}, "in plain sight not painted white on white"},
	} {
		for _, format := range []string{"text", "markdown"} {
			out, stderr, status := command(t, append(append([]string{"-quiet", "-format", format}, c.args...), path)...)
			if got := strings.Join(strings.Fields(out), " "); status != 0 || got != c.want {
				t.Errorf("%s %v: status %d, %q, want %q (%s)", format, c.args, status, got, c.want, stderr)
			}
		}
	}
}

func TestImagesAndWorkers(t *testing.T) {
	b := pdftest.New()
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x80"))
	pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{
		Content:   "q 100 0 0 100 72 500 cm /Im Do Q BT /F1 12 Tf 72 720 Td (a picture) Tj ET",
		Resources: "/Font << /F1 4 0 R >> /XObject << /Im 1 0 R >>",
	})
	path := write(t, "image.pdf", b.Bytes())
	images := func(mode string) (blocks, withData int) {
		stdout, stderr, status := command(t, "-images", mode, "-workers", "3", path)
		if status != 0 {
			t.Fatalf("-images %s: status %d, %s", mode, status, stderr)
		}
		for _, b := range document(t, stdout).Parts[0].Blocks {
			if b.Kind == gofastcore.KindImage {
				blocks++
				if len(b.Image.Data) > 0 {
					withData++
				}
			}
		}
		return blocks, withData
	}
	for mode, want := range map[string][2]int{"none": {0, 0}, "meta": {1, 0}, "data": {1, 1}} {
		if blocks, data := images(mode); blocks != want[0] || data != want[1] {
			t.Errorf("-images %s: %d image blocks, %d with data", mode, blocks, data)
		}
	}
}

func TestOutputFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.json")
	stdout, stderr, status := command(t, "-o", out, filepath.Join(fixtures, "english.pdf"))
	if status != 0 || stdout != "" || stderr != "" {
		t.Fatalf("status %d, stdout %q, stderr %q", status, stdout, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	document(t, string(data))
	if _, stderr, status := command(t, "-o", filepath.Join(t.TempDir(), "no", "such", "dir", "out.json"), filepath.Join(fixtures, "english.pdf")); status != 1 || stderr == "" {
		t.Errorf("an output file that cannot be made: status %d, %q", status, stderr)
	}
}

func TestCommandLine(t *testing.T) {
	english := filepath.Join(fixtures, "english.pdf")
	for _, args := range [][]string{
		{}, {"-format", "html", english}, {"-images", "all", english}, {"-pages", "x", english}, {"-pages", "5-2", english},
		{"-pages", "0", english}, {"-pages", "-", english}, {"-workers", "-1", english}, {"-exclude-hidden=nonsense", english},
		{"-no-such-flag", english},
	} {
		if stdout, stderr, status := command(t, args...); status != 2 || stdout != "" || stderr == "" {
			t.Errorf("%v: status %d, stdout %.40q, stderr %.60q", args, status, stdout, stderr)
		}
	}
	if stdout, _, status := command(t, "-version"); status != 0 || !strings.HasPrefix(stdout, "gofastpdf ") {
		t.Errorf("-version: status %d, %q", status, stdout)
	}
	if _, stderr, status := command(t, "-h"); status != 0 || !strings.Contains(stderr, "usage: gofastpdf") {
		t.Errorf("-h: status %d, %.60q", status, stderr)
	}
	// A file that is not a PDF: a message, status 1, nothing written.
	notPDF := write(t, "notes.pdf", []byte("just some text"))
	if stdout, stderr, status := command(t, notPDF); status != 1 || stdout != "" || !strings.Contains(stderr, "notes.pdf") {
		t.Errorf("not a PDF: status %d, stdout %q, stderr %q", status, stdout, stderr)
	}
}

// TestCancelled: a context that has ended stops the command, with
// status 1.
func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errs bytes.Buffer
	if status := run(ctx, []string{filepath.Join(fixtures, "english.pdf")}, &out, &errs); status != 1 || out.Len() != 0 {
		t.Errorf("status %d, output %.40q, %q", status, out.String(), errs.String())
	}
}
