package gofastpdf_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// One test per defect of IMPLEMENTATION_PLAN.md section 2: correctness
// (B) and crashes, hangs and resource exhaustion (C). The cases that
// were still open used to be listed in a register, which is gone: all
// are closed.

// textCase checks that data extracts to the words in want, as the defect
// case key, or as an ordinary test when key is empty. With oracle set,
// pdftotext must read the same words from the file, which proves that the
// expectation is a property of the file and not of this test.
func textCase(t *testing.T, key string, data []byte, want string, oracle bool) {
	t.Helper()
	if oracle && pdftest.HavePdftotext() {
		got := "(pdftotext failed)"
		pages, err := pdftest.Pdftotext(pdftest.TempFile(t, data))
		if err == nil {
			got = strings.Join(pdftest.Words(strings.Join(pages, "\n")), " ")
		}
		if got != want {
			// Releases of the reference reader differ in what they
			// support. The expectations were confirmed against the one
			// recorded in testdata/baseline-public.ref; another release
			// disagreeing does not make them wrong.
			if oracleIsReference() {
				t.Fatalf("%s: the test expects %q but pdftotext reads %q (%v)", key, want, got, err)
			}
			t.Logf("%s: this machine's pdftotext reads %q where the test expects %q", key, got, want)
		}
	}
	check(t, key, wantText(data, want))
}

var oracleIsReference = sync.OnceValue(func() bool {
	recorded, err := os.ReadFile("testdata/baseline-public.ref")
	return err == nil && strings.TrimSpace(string(recorded)) == referenceVersion()
})

const withOracle, noOracle = true, false

func TestB1IndirectLength(t *testing.T) {
	// The content stream's /Length is an indirect reference, and the stream
	// is longer than the reader's buffer.
	body := "BT /F1 12 Tf 72 720 Td (START) Tj ET\n" + strings.Repeat("0 0 m 10 10 l S\n", 600) + "BT /F1 12 Tf 72 700 Td (END) Tj ET"
	b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{})
	length := b.Add(fmt.Sprint(len(body)))
	b.Set(4, fmt.Sprintf("<< /Length %d 0 R >>\nstream\n%s\nendstream", length, body))
	textCase(t, "B1", b.Bytes(), "START END", withOracle)
}

func TestB2Spacing(t *testing.T) {
	t.Run("tm-scale", func(t *testing.T) {
		// Font size 1 scaled by the text matrix; small kerning must not
		// split the words.
		data := pdftest.OnePage("BT /F1 1 Tf 12 0 0 12 72 720 Tm [(Hel) -20 (lo W) 15 (orld)] TJ ET")
		textCase(t, "B2/tm-scale", data, "Hello World", withOracle)
	})
	t.Run("ctm-scale", func(t *testing.T) {
		// Everything scaled by the CTM; the second Td is a new line.
		data := pdftest.OnePage("0.1 0 0 0.1 0 0 cm BT /F1 120 Tf 720 7200 Td (Line one) Tj 0 -140 Td (Line two) Tj ET")
		textCase(t, "B2/ctm-scale", data, "Line one Line two", withOracle)
	})
}

func TestB3WinAnsi(t *testing.T) {
	data := pdftest.OnePage(`BT /F1 12 Tf 72 720 Td (caf\351 \223quoted\224 na\357ve \226 100\200) Tj ET`)
	textCase(t, "B3", data, "café “quoted” naïve – 100€", withOracle)
}

func TestB4Inheritance(t *testing.T) {
	// /Resources and /MediaBox are on the page tree node, not on the page.
	b := pdftest.New()
	catalog, tree, page := b.Reserve(), b.Reserve(), b.Reserve()
	font := b.Add(pdftest.FontHelvetica)
	content := b.AddStream("", []byte("BT /F1 12 Tf 72 720 Td (Inherited) Tj ET"))
	b.Set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", tree))
	b.Set(tree, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R >> >> >>", page, font))
	b.Set(page, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /Contents %d 0 R >>", tree, content))
	data := b.Bytes()

	t.Run("resources", func(t *testing.T) {
		// The font maps code 1 to "Hi" through a ToUnicode CMap, so the
		// text comes out only if the inherited font is found.
		tu := b.AddStream("", []byte(toUnicodeCMap("<00> <FF>", "<01> <00480069>")))
		b.Set(font, fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode %d 0 R >>", tu))
		b.Set(content, pdftest.Stream("", "BT /F1 12 Tf 72 720 Td <01> Tj ET"))
		textCase(t, "B4/resources", b.Bytes(), "Hi", withOracle)
	})
	t.Run("mediabox", func(t *testing.T) {
		pdftest.Defect(t, "B4/mediabox", func() error {
			e, err := extractBytes(data, extractOpts{})
			if err != nil {
				return err
			}
			if len(e.Pages) != 1 || e.Pages[0].Width != 595 || e.Pages[0].Height != 842 {
				return fmt.Errorf("page size is %+v, want one page of 595x842", e.Pages)
			}
			return nil
		})
	})
}

func TestB5FormXObjectText(t *testing.T) {
	b := pdftest.New()
	font := b.Add(pdftest.FontHelvetica)
	form := b.AddStream(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >>", font),
		[]byte("BT /F1 12 Tf 72 700 Td (InsideForm) Tj ET"))
	pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{
		Content:   "BT /F1 12 Tf 72 720 Td (Body) Tj ET /Fm1 Do",
		Resources: fmt.Sprintf("/Font << /F1 %d 0 R >> /XObject << /Fm1 %d 0 R >>", font, form),
	})
	textCase(t, "B5", b.Bytes(), "Body InsideForm", withOracle)
}

func TestB6StrayByteInContent(t *testing.T) {
	// One byte that is not valid in a content stream must not cost the
	// rest of the page.
	data := pdftest.OnePage("BT /F1 12 Tf 72 720 Td (before) Tj } 0 -20 Td (after) Tj ET")
	textCase(t, "B6", data, "before after", withOracle)
}

func TestB7NothingOnStdout(t *testing.T) {
	// /Resources points at an object that does not exist, which makes the
	// library warn. A library must not write to standard output.
	b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: pdftest.TextPage("hi")})
	b.Set(5, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 99 0 R /Contents 4 0 R >>")
	data := b.Bytes()
	pdftest.Defect(t, "B7", func() error {
		out := captureStdout(func() { extractBytes(data, extractOpts{}) })
		if out != "" {
			return fmt.Errorf("the library wrote to stdout: %q", out)
		}
		return nil
	})
}

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.String()
	}()
	func() {
		defer func() { os.Stdout = old; w.Close() }()
		fn()
	}()
	return <-done
}

func TestB8FailedPageIsReported(t *testing.T) {
	// The second of three pages is not a page at all. The other two must
	// come back, and the second must be present and marked as failed, so
	// that page numbers stay true and the caller knows something is missing.
	b := pdftest.Doc(pdftest.DocOptions{},
		pdftest.PageSpec{Content: pdftest.TextPage("one")},
		pdftest.PageSpec{Content: pdftest.TextPage("two")},
		pdftest.PageSpec{Content: pdftest.TextPage("three")})
	b.Set(7, "42") // object 7 is the second page's dictionary
	data := b.Bytes()
	pdftest.Defect(t, "B8", func() error {
		e, err := extractBytes(data, extractOpts{})
		if err != nil {
			return fmt.Errorf("the whole document failed: %w", err)
		}
		if len(e.Pages) != 3 {
			return fmt.Errorf("got %d pages, want 3 with the second marked failed", len(e.Pages))
		}
		if !e.Pages[1].Failed || e.Pages[0].Failed || e.Pages[2].Failed {
			return errors.New("the second page is not marked failed")
		}
		if got := e.Pages[0].Text + "|" + e.Pages[2].Text; strings.Join(pdftest.Words(got), " ") != "one |three" {
			return fmt.Errorf("the readable pages are %q", got)
		}
		return nil
	})
}

func TestB9SaveRestoreTextState(t *testing.T) {
	// The font is part of the graphics state: after Q the font selected
	// before q is current again. /F2 maps code 65 to Z.
	b := pdftest.New()
	f1 := b.Add(pdftest.FontHelvetica)
	f2 := b.Add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [65 /Z] >> >>")
	pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{
		Content:   "/F1 12 Tf q /F2 12 Tf Q BT 72 720 Td (A) Tj ET",
		Resources: fmt.Sprintf("/Font << /F1 %d 0 R /F2 %d 0 R >>", f1, f2),
	})
	textCase(t, "B9", b.Bytes(), "A", withOracle)
}

func TestB10InfoStrings(t *testing.T) {
	info := func(title, author string) []byte {
		return pdftest.Doc(pdftest.DocOptions{Configure: func(b *pdftest.Builder) {
			n := b.Add(fmt.Sprintf("<< /Title %s /Author %s >>", title, author))
			b.Trailer = fmt.Sprintf("/Info %d 0 R", n)
		}}, pdftest.PageSpec{Content: pdftest.TextPage("x")}).Bytes()
	}
	check := func(t *testing.T, key string, data []byte) {
		pdftest.Defect(t, key, func() error {
			e, err := extractBytes(data, extractOpts{})
			if err != nil {
				return err
			}
			if e.Title != "Résumé" || e.Author != "Zoë" {
				return fmt.Errorf("title %q, author %q; want %q and %q", e.Title, e.Author, "Résumé", "Zoë")
			}
			return nil
		})
	}
	t.Run("utf16", func(t *testing.T) {
		// UTF-16BE with a byte order mark, one as a hex string.
		check(t, "B10/utf16", info("<FEFF005200E900730075006D00E9>", "(\xfe\xff\x00Z\x00o\x00\xeb)"))
	})
	t.Run("pdfdoc", func(t *testing.T) {
		check(t, "B10/pdfdoc", info(`(R\351sum\351)`, `(Zo\353)`))
	})
}

func TestB11CharCountIsCharacters(t *testing.T) {
	// "é" is one character and two bytes of UTF-8. The mapping comes from
	// a ToUnicode CMap so that the test does not depend on B3.
	b := pdftest.New()
	tu := b.AddStream("", []byte(toUnicodeCMap("<00> <FF>", "<01> <00E9>")))
	data := pdftest.Doc(pdftest.DocOptions{Base: b,
		Font: fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode %d 0 R >>", tu)},
		pdftest.PageSpec{Content: "BT /F1 12 Tf 72 720 Td <010101> Tj ET"}).Bytes()
	pdftest.Defect(t, "B11", func() error {
		e, err := extractBytes(data, extractOpts{})
		if err != nil {
			return err
		}
		if len(e.Pages) != 1 || strings.TrimSpace(e.Pages[0].Text) != "ééé" {
			return fmt.Errorf("pages are %+v, want the text ééé", e.Pages)
		}
		if n := e.Pages[0].Chars - strings.Count(e.Pages[0].Text, "\n"); n != 3 {
			return fmt.Errorf("character count is %d for 3 characters", e.Pages[0].Chars)
		}
		return nil
	})
}

// toUnicodeCMap returns a ToUnicode CMap with one codespace range and the
// given bfchar lines.
func toUnicodeCMap(codespace string, bfchars ...string) string {
	return fmt.Sprintf("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n"+
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n"+
		"1 begincodespacerange\n%s\nendcodespacerange\n%d beginbfchar\n%s\nendbfchar\n"+
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend",
		codespace, len(bfchars), strings.Join(bfchars, "\n"))
}

func TestC1MissingOperands(t *testing.T) {
	// Operators without their operands must be skipped, not crash, and the
	// valid text around them must survive.
	cases := map[string]string{
		"tf":         "BT /F1 12 Tf 72 720 Td (ok) Tj Tf ET",
		"tc":         "BT /F1 12 Tf 72 720 Td (ok) Tj Tc ET",
		"tw":         "BT /F1 12 Tf 72 720 Td (ok) Tj Tw ET",
		"td":         "BT /F1 12 Tf 72 720 Td (ok) Tj Td ET",
		"tm":         "BT /F1 12 Tf 72 720 Td (ok) Tj 1 0 Tm ET",
		"tj":         "BT /F1 12 Tf 72 720 Td (ok) Tj Tj ET",
		"tj-array":   "BT /F1 12 Tf 72 720 Td (ok) Tj TJ ET",
		"quote":      "BT /F1 12 Tf 72 720 Td (ok) Tj \" ET",
		"cm":         "BT /F1 12 Tf 72 720 Td (ok) Tj ET 1 0 cm",
		"do":         "BT /F1 12 Tf 72 720 Td (ok) Tj ET Do",
		"wrong-type": "BT /F1 12 Tf 72 720 Td (ok) Tj (a) (b) Td /X /Y Tf [1] Tj ET",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			textCase(t, "C1/"+name, pdftest.OnePage(content), "ok", noOracle)
		})
	}
}

func TestC2TruncatedInput(t *testing.T) {
	t.Run("array", func(t *testing.T) {
		// The content stream ends inside an array.
		textCase(t, "C2/array", pdftest.OnePage("BT /F1 12 Tf 72 720 Td (ok) Tj [(a) (b)"), "ok", noOracle)
	})
	t.Run("no-type", func(t *testing.T) {
		// Page tree nodes without /Type are common in the wild.
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: pdftest.TextPage("hi")})
		b.Set(2, "<< /Kids [5 0 R] /Count 1 >>")
		b.Set(5, "<< /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents 4 0 R >>")
		textCase(t, "C2/no-type", b.Bytes(), "hi", withOracle)
	})
}

func TestC3UnboundedRecursion(t *testing.T) {
	t.Run("form-self", func(t *testing.T) {
		// A Form XObject that draws itself.
		b := pdftest.New()
		font := b.Add(pdftest.FontHelvetica)
		form := b.Reserve()
		b.Set(form, pdftest.Stream(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /XObject << /Fm1 %d 0 R >> >>", form), "/Fm1 Do"))
		pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{
			Content:   "BT /F1 12 Tf 72 720 Td (Body) Tj ET /Fm1 Do",
			Resources: fmt.Sprintf("/Font << /F1 %d 0 R >> /XObject << /Fm1 %d 0 R >>", font, form),
		})
		data := b.Bytes()
		pdftest.DefectIsolated(t, "C3/form-self", 30*time.Second, func() error {
			e, err := extractBytes(data, extractOpts{Images: true})
			if err != nil {
				return err
			}
			if e.words() != "Body" {
				return fmt.Errorf("text is %q, want %q", e.words(), "Body")
			}
			return nil
		})
	})
	t.Run("page-tree-cycle", func(t *testing.T) {
		// The page tree node lists itself as a kid.
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: pdftest.TextPage("page")})
		b.Set(2, "<< /Type /Pages /Kids [2 0 R 5 0 R] /Count 2 >>")
		data := b.Bytes()
		pdftest.DefectIsolated(t, "C3/page-tree-cycle", 30*time.Second, func() error {
			// Any answer is acceptable except a crash or a hang; if pages
			// come back, the real one must be among them.
			e, err := extractBytes(data, extractOpts{})
			if err == nil && !strings.Contains(e.words(), "page") {
				return fmt.Errorf("text is %q, want the real page or an error", e.words())
			}
			return nil
		})
	})
	t.Run("deep-nesting", func(t *testing.T) {
		// A million nested arrays in the page dictionary. The child process
		// runs with a 64 MB stack limit, so this checks that stack use does
		// not grow with the nesting depth of the input.
		const depth = 1_000_000
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{
			Content: pdftest.TextPage("deep"),
			Extra:   "/X " + strings.Repeat("[", depth) + strings.Repeat("]", depth),
		})
		data := b.Bytes()
		pdftest.DefectIsolated(t, "C3/deep-nesting", 60*time.Second, func() error {
			extractBytes(data, extractOpts{}) // an error is fine; a crash is not
			return nil
		})
	})
}

func TestC4CMapWithProcedure(t *testing.T) {
	// A brace in a ToUnicode CMap (legal PostScript) must not hang the
	// parser, and the mappings after it must still apply.
	cmap := "/CMapName /X def { pop } bind def\n1 begincodespacerange <00> <FF> endcodespacerange\n1 beginbfchar <01> <0041> endbfchar"
	b := pdftest.New()
	tu := b.AddStream("", []byte(cmap))
	data := pdftest.Doc(pdftest.DocOptions{Base: b,
		Font: fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode %d 0 R >>", tu)},
		pdftest.PageSpec{Content: "BT /F1 12 Tf 72 720 Td <01> Tj ET"}).Bytes()
	pdftest.DefectIsolated(t, "C4", 5*time.Second, wantText(data, "A"))
}

func TestC6TrustedSizes(t *testing.T) {
	const timeout = 60 * time.Second
	finishes := func(data []byte, maxAlloc uint64) func() error {
		return func() error {
			var err error
			n := pdftest.AllocBytes(func() { _, err = extractBytes(data, extractOpts{}) })
			if n > maxAlloc {
				return fmt.Errorf("allocated %d MB for a %d KB file (result: %v)", n>>20, len(data)>>10, err)
			}
			return nil
		}
	}
	t.Run("length", func(t *testing.T) {
		// A stream that claims to be a terabyte long.
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{})
		b.Set(4, "<< /Length 1000000000000 >>\nstream\n"+pdftest.TextPage("ok")+"\nendstream")
		pdftest.DefectIsolated(t, "C6/length", timeout, finishes(b.Bytes(), 64<<20))
	})
	t.Run("count", func(t *testing.T) {
		// One page, and a page tree that claims a billion.
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: pdftest.TextPage("ok")})
		b.Set(2, "<< /Type /Pages /Kids [5 0 R] /Count 1000000000 >>")
		data := b.Bytes()
		pdftest.DefectIsolated(t, "C6/count", 5*time.Second, func() error {
			e, err := extractBytes(data, extractOpts{})
			if err != nil {
				return err
			}
			if len(e.Pages) != 1 || e.words() != "ok" {
				return fmt.Errorf("got %d pages, text %q; want the one real page", len(e.Pages), e.words())
			}
			return nil
		})
	})
	t.Run("bfrange", func(t *testing.T) {
		// A ToUnicode range that spans four billion codes.
		cmap := "1 begincodespacerange <00000000> <FFFFFFFF> endcodespacerange\n1 beginbfrange <00000000> <FFFFFFFF> <0041> endbfrange"
		b := pdftest.New()
		tu := b.AddStream("", []byte(cmap))
		data := pdftest.Doc(pdftest.DocOptions{Base: b,
			Font: fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode %d 0 R >>", tu)},
			pdftest.PageSpec{Content: "BT /F1 12 Tf 72 720 Td <00000000> Tj ET"}).Bytes()
		pdftest.DefectIsolated(t, "C6/bfrange", 5*time.Second, finishes(data, 64<<20))
	})
	t.Run("inflate", func(t *testing.T) {
		// A content stream of 250 KB that inflates to 256 MB. The reader
		// stops at its limit for one stream (64 MiB by default), so what
		// it allocates is bounded by a small multiple of that limit and
		// not by what the file claims.
		content := append([]byte(pdftest.TextPage("ok")), bytes.Repeat([]byte(" "), 256<<20)...)
		data := pdftest.Doc(pdftest.DocOptions{Compress: true}, pdftest.PageSpec{Content: string(content)}).Bytes()
		content = nil
		pdftest.DefectIsolated(t, "C6/inflate", timeout, func() error {
			var e *extraction
			var err error
			n := pdftest.AllocBytes(func() { e, err = extractBytes(data, extractOpts{}) })
			if n > 160<<20 {
				return fmt.Errorf("allocated %d MB for a %d KB file", n>>20, len(data)>>10)
			}
			if err != nil || e.words() != "ok" {
				return fmt.Errorf("text %q, error %v; want the text before the limit", e.words(), err)
			}
			return nil
		})
	})
}

func TestC7WrongTypes(t *testing.T) {
	// Each case puts a value of the wrong type where the library expects a
	// dictionary, an array, a number or a stream. An error is the right
	// answer; a panic is not.
	doc := func(edit func(b *pdftest.Builder)) []byte {
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: pdftest.TextPage("x")})
		edit(b)
		return b.Bytes()
	}
	page := func(entries string) string {
		return "<< /Type /Page /Parent 2 0 R " + entries + " >>"
	}
	const ok = "/MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents 4 0 R"
	cases := map[string][]byte{
		"root-number":      doc(func(b *pdftest.Builder) { b.Set(1, "42") }),
		"root-missing":     doc(func(b *pdftest.Builder) { b.Root = 99 }),
		"pages-number":     doc(func(b *pdftest.Builder) { b.Set(1, "<< /Type /Catalog /Pages 42 >>") }),
		"pages-missing":    doc(func(b *pdftest.Builder) { b.Set(1, "<< /Type /Catalog >>") }),
		"kids-number":      doc(func(b *pdftest.Builder) { b.Set(2, "<< /Type /Pages /Kids 5 /Count 1 >>") }),
		"kids-of-numbers":  doc(func(b *pdftest.Builder) { b.Set(2, "<< /Type /Pages /Kids [1 2 3] /Count 3 >>") }),
		"count-name":       doc(func(b *pdftest.Builder) { b.Set(2, "<< /Type /Pages /Kids [5 0 R] /Count /One >>") }),
		"contents-number":  doc(func(b *pdftest.Builder) { b.Set(5, page("/MediaBox [0 0 612 792] /Contents 7")) }),
		"contents-dict":    doc(func(b *pdftest.Builder) { b.Set(4, "<< /A 1 >>") }),
		"contents-array":   doc(func(b *pdftest.Builder) { b.Set(5, page("/MediaBox [0 0 612 792] /Contents [1 (a) /B 4 0 R]")) }),
		"mediabox-strings": doc(func(b *pdftest.Builder) { b.Set(5, page("/MediaBox [(a) (b) (c) (d)] /Contents 4 0 R")) }),
		"mediabox-short":   doc(func(b *pdftest.Builder) { b.Set(5, page("/MediaBox [0 0] /Contents 4 0 R")) }),
		"resources-array":  doc(func(b *pdftest.Builder) { b.Set(5, page("/MediaBox [0 0 612 792] /Resources [1 2] /Contents 4 0 R")) }),
		"font-number":      doc(func(b *pdftest.Builder) { b.Set(3, "12") }),
		"fonts-number": doc(func(b *pdftest.Builder) {
			b.Set(5, page("/MediaBox [0 0 612 792] /Resources << /Font 7 >> /Contents 4 0 R"))
		}),
		"encoding-number": doc(func(b *pdftest.Builder) {
			b.Set(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding 5 >>")
		}),
		"differences-dict": doc(func(b *pdftest.Builder) {
			b.Set(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /Differences << /A 1 >> >> >>")
		}),
		"widths-names": doc(func(b *pdftest.Builder) {
			b.Set(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar /A /LastChar (z) /Widths [/a /b] >>")
		}),
		"tounicode-number": doc(func(b *pdftest.Builder) {
			b.Set(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode 9 >>")
		}),
		"info-number":   doc(func(b *pdftest.Builder) { b.Trailer = "/Info 3" }),
		"info-values":   doc(func(b *pdftest.Builder) { b.Set(50, "<< /Title 12 /Author [1] >>"); b.Trailer = "/Info 50 0 R" }),
		"length-name":   doc(func(b *pdftest.Builder) { b.Set(4, "<< /Length /Big >>\nstream\n"+pdftest.TextPage("x")+"\nendstream") }),
		"filter-number": doc(func(b *pdftest.Builder) { b.Set(4, pdftest.Stream("/Filter 7", pdftest.TextPage("x"))) }),
		"xobject-number": doc(func(b *pdftest.Builder) {
			b.Set(4, pdftest.Stream("", "/Im1 Do"))
			b.Set(5, page("/MediaBox [0 0 612 792] /Resources << /XObject << /Im1 12 >> >> /Contents 4 0 R"))
		}),
		"encrypt-number": doc(func(b *pdftest.Builder) { b.Trailer = "/Encrypt 12" }),
		"page-ok":        doc(func(b *pdftest.Builder) { b.Set(5, page(ok)) }),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			pdftest.DefectIsolated(t, "C7/"+name, 30*time.Second, func() error {
				extractBytes(data, extractOpts{Images: true})
				return nil
			})
		})
	}
}
