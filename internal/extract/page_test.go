package extract

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

func open(t testing.TB, data []byte, limits file.Limits) *file.File {
	t.Helper()
	f, err := file.Open(data, limits)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return f
}

// extractAll extracts every page and fails the test on a page error.
func extractAll(t testing.TB, f *file.File, opts Options) []Result {
	t.Helper()
	pages := f.Pages()
	out := make([]Result, len(pages))
	for i := range pages {
		out[i] = Page(f, &pages[i], opts)
		if out[i].Err != nil {
			t.Fatalf("page %d: %v", i+1, out[i].Err)
		}
	}
	return out
}

func onePage(t testing.TB, content string) Result {
	t.Helper()
	return extractAll(t, open(t, pdftest.OnePage(content), file.Limits{}), Options{})[0]
}

func codes(ws []gofastcore.Warning) string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Code)
	}
	return strings.Join(out, ",")
}

func TestText(t *testing.T) {
	cases := []struct{ name, content, want string }{
		{"one string", "BT /F1 12 Tf 72 720 Td (Hello) Tj ET", "Hello"},
		{"two lines", "BT /F1 12 Tf 72 720 Td (one) Tj 0 -14 Td (two) Tj ET", "one\ntwo"},
		{"T* and leading", "BT /F1 12 Tf 14 TL 72 720 Td (one) Tj T* (two) Tj ET", "one\ntwo"},
		{"quote operators", "BT /F1 12 Tf 14 TL 72 720 Td (one) Tj (two) ' 1 2 (three) \" ET", "one\ntwo\nthree"},
		{"TJ kerning keeps the word", "BT /F1 12 Tf 72 720 Td [(Hel) -20 (lo)] TJ ET", "Hello"},
		{"TJ gap makes a space", "BT /F1 12 Tf 72 720 Td [(Hello) -900 (World)] TJ ET", "Hello World"},
		{"hex string", "BT /F1 12 Tf 72 720 Td <48656C6C6F> Tj ET", "Hello"},
		{"escapes", `BT /F1 12 Tf 72 720 Td (a\(b\)c\101) Tj ET`, "a(b)cA"},
		{"no font", "BT 72 720 Td (plain) Tj ET", "plain"},
		{"unknown font name", "BT /Nope 12 Tf 72 720 Td (plain) Tj ET", "plain"},
		{"font name with an escape", "BT /F#31 12 Tf 72 720 Td (escaped) Tj ET", "escaped"},
		{"q and Q", "q 2 0 0 2 0 0 cm Q BT /F1 12 Tf 72 720 Td (x) Tj ET", "x"},
		{"unbalanced Q", "Q Q Q BT /F1 12 Tf 72 720 Td (x) Tj ET", "x"},
		{"empty page", "", ""},
		{"only paths", "0 0 m 100 100 l S 1 2 3 4 re f", ""},
	}
	for _, c := range cases {
		if got := onePage(t, c.content).Text; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// Defect C1: an operator without its operands is skipped; the text
// around it survives and nothing panics.
func TestMissingAndWrongOperands(t *testing.T) {
	ops := []string{"Tf", "Tc", "Tw", "Tz", "TL", "Td", "TD", "Tm", "T*", "Tj", "TJ", "'", "\"", "cm", "Do", "BI", "q", "Q", "BT", "ET"}
	operands := []string{"", "1", "(s)", "/N", "[1 (a)]", "<< /A 1 >>", "1 2 3 4 5", "(a) (b) /C [1] 5 true", "1 2 3 4 5 6 7 8"}
	for _, op := range ops {
		for _, args := range operands {
			content := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (before) Tj %s %s ET", args, op)
			res := onePage(t, content)
			if !strings.HasPrefix(res.Text, "before") {
				t.Errorf("%q: text %q lost what came before", args+" "+op, res.Text)
			}
		}
	}
}

func TestContentsArray(t *testing.T) {
	// Three streams; the operands of Tm and of Tj arrive in different
	// streams from their operators.
	b := pdftest.New()
	font := b.Add(pdftest.FontHelvetica)
	s1 := b.AddStream("", []byte("BT /F1 12 Tf 1 0 0 1"))
	s2 := b.AddFlate("", []byte(" 72 720 Tm (first)"))
	s3 := b.AddStream("", []byte(" Tj 0 -14 Td (second) Tj ET"))
	missing := 999
	pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Resources: fmt.Sprintf("/Font << /F1 %d 0 R >>", font)})
	f := open(t, b.Bytes(), file.Limits{})
	page := f.Pages()[0]
	// Point /Contents at the three streams, a missing object and a number.
	b.Set(int(page.Obj), fmt.Sprintf("<< /Type /Page /Parent 5 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> /Contents [%d 0 R %d 0 R %d 0 R %d 0 R 42] >>",
		font, s1, s2, missing, s3))
	f = open(t, b.Bytes(), file.Limits{})
	if got := extractAll(t, f, Options{})[0].Text; got != "first\nsecond" {
		t.Errorf("text %q", got)
	}
}

func TestDamagedContent(t *testing.T) {
	t.Run("stray bytes", func(t *testing.T) {
		res := onePage(t, "BT /F1 12 Tf 72 720 Td (ok) Tj } ) > 0 -14 Td (kept) Tj ET")
		if res.Text != "ok\nkept" || codes(res.Warnings) != "pdf.content_bytes_skipped" {
			t.Errorf("text %q, warnings %s", res.Text, codes(res.Warnings))
		}
	})
	t.Run("damaged flate keeps the prefix", func(t *testing.T) {
		var lines []string
		for i := range 500 {
			lines = append(lines, fmt.Sprintf("line %03d", i))
		}
		z := pdftest.Deflate([]byte(pdftest.TextPage(lines...)))
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{})
		b.Set(4, pdftest.Stream("/Filter /FlateDecode", string(z[:len(z)/2])))
		res := extractAll(t, open(t, b.Bytes(), file.Limits{}), Options{})[0]
		if !strings.HasPrefix(res.Text, "line 000\nline 001") || codes(res.Warnings) != "pdf.content_stream_damaged" {
			t.Errorf("text starts %.30q, warnings %s", res.Text, codes(res.Warnings))
		}
	})
	t.Run("unsupported filter", func(t *testing.T) {
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{})
		b.Set(4, pdftest.Stream("/Filter /LZWDecode", "xx"))
		res := extractAll(t, open(t, b.Bytes(), file.Limits{}), Options{})[0]
		if res.Text != "" || codes(res.Warnings) != "pdf.content_stream_unreadable" {
			t.Errorf("text %q, warnings %s", res.Text, codes(res.Warnings))
		}
	})
	t.Run("invalid bytes become U+FFFD", func(t *testing.T) {
		font := "<< /Type /Font /Subtype /Type1 /Encoding << /Differences [65 /NoSuchGlyphName] >> >>"
		data := pdftest.Doc(pdftest.DocOptions{Font: font}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 720 Td (a\\351\\352b) Tj ET"}).Bytes()
		res := extractAll(t, open(t, data, file.Limits{}), Options{})[0]
		if res.Text != "a��b" || codes(res.Warnings) != gofastcore.WarnTextUnmappedGlyphs {
			t.Errorf("text %q, warnings %s", res.Text, codes(res.Warnings))
		}
	})
	t.Run("page tree entry that is not a page", func(t *testing.T) {
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: "x"}, pdftest.PageSpec{Content: "y"})
		b.Set(5, "42")
		f := open(t, b.Bytes(), file.Limits{})
		if res := Page(f, &f.Pages()[0], Options{}); res.Err == nil || res.Text != "" {
			t.Errorf("result %+v, want an error", res)
		}
	})
}

func imageDoc(t testing.TB, body string, objects func(b *pdftest.Builder) string) *file.File {
	b := pdftest.New()
	resources := objects(b)
	return open(t, pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: body, Resources: resources}).Bytes(), file.Limits{})
}

func TestImages(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		if res := onePage(t, "BI /W 1 /H 1 ID x EI"); res.Images != nil {
			t.Errorf("images %v without the option", res.Images)
		}
	})
	t.Run("XObject and inline", func(t *testing.T) {
		f := imageDoc(t, "q 200 0 0 100 72 500 cm /Im1 Do Q BI /W 4 /H 2 /CS /RGB /BPC 8 ID abc\nEI /Missing Do /Im1 Do",
			func(b *pdftest.Builder) string {
				img := b.AddStream("/Type /XObject /Subtype /Image /Width 640 /Height 480 /ColorSpace /DeviceRGB /BitsPerComponent 8", []byte("not decoded"))
				return fmt.Sprintf("/XObject << /Im1 %d 0 R >>", img)
			})
		got := extractAll(t, f, Options{Images: true})[0].Images
		want := []Image{
			{Name: "/Im1", Rect: [4]float64{72, 500, 200, 100}, Width: 640, Height: 480, ColorSpace: "/DeviceRGB"},
			{Inline: true, Rect: [4]float64{0, 0, 1, 1}, Width: 4, Height: 2, ColorSpace: "/RGB"},
			{Name: "/Im1", Rect: [4]float64{0, 0, 1, 1}, Width: 640, Height: 480, ColorSpace: "/DeviceRGB"},
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("got  %+v\nwant %+v", got, want)
		}
	})
	t.Run("image data is never decoded", func(t *testing.T) {
		// A corrupt stream with a filter this reader does not have, and
		// one that would inflate to 64 MB: both are listed from their
		// dictionaries alone (defect P4).
		f := imageDoc(t, "/Bad Do /Big Do", func(b *pdftest.Builder) string {
			bad := b.AddStream("/Type /XObject /Subtype /Image /Width 10 /Height 10 /Filter /JBIG2Decode", []byte("garbage"))
			big := b.AddFlate("/Type /XObject /Subtype /Image /Width 8192 /Height 8192", make([]byte, 64<<20))
			return fmt.Sprintf("/XObject << /Bad %d 0 R /Big %d 0 R >>", bad, big)
		})
		var res Result
		n := pdftest.AllocBytes(func() { res = Page(f, &f.Pages()[0], Options{Images: true}) })
		if len(res.Images) != 2 || res.Images[1].Width != 8192 || len(res.Warnings) != 0 {
			t.Errorf("images %+v, warnings %s", res.Images, codes(res.Warnings))
		}
		if n > 1<<20 {
			t.Errorf("allocated %d KB to list two images", n>>10)
		}
	})
	t.Run("images inside forms", func(t *testing.T) {
		f := imageDoc(t, "/Outer Do", func(b *pdftest.Builder) string {
			img := b.AddStream("/Type /XObject /Subtype /Image /Width 3 /Height 4", nil)
			inner := b.AddStream(fmt.Sprintf("/Type /XObject /Subtype /Form /Resources << /XObject << /I %d 0 R >> >>", img), []byte("/I Do /I Do"))
			outer := b.AddFlate(fmt.Sprintf("/Type /XObject /Subtype /Form /Resources << /XObject << /In %d 0 R /I %d 0 R >> >>", inner, img), []byte("/In Do /I Do"))
			return fmt.Sprintf("/XObject << /Outer %d 0 R >>", outer)
		})
		if got := extractAll(t, f, Options{Images: true})[0].Images; len(got) != 3 {
			t.Errorf("%d images, want 3", len(got))
		}
	})
}

// Defect C3: forms that draw themselves or each other, and a tree of
// forms that doubles at every level, all end quickly.
func TestFormRecursionIsBounded(t *testing.T) {
	t.Run("draws itself", func(t *testing.T) {
		f := imageDoc(t, "BT /F1 12 Tf 72 720 Td (Body) Tj ET /Fm Do", func(b *pdftest.Builder) string {
			font := b.Add(pdftest.FontHelvetica)
			form := b.Reserve()
			b.Set(form, pdftest.Stream(fmt.Sprintf("/Type /XObject /Subtype /Form /Resources << /XObject << /Fm %d 0 R >> >>", form), "/Fm Do /Fm Do"))
			return fmt.Sprintf("/Font << /F1 %d 0 R >> /XObject << /Fm %d 0 R >>", font, form)
		})
		if res := extractAll(t, f, Options{Images: true})[0]; res.Text != "Body" {
			t.Errorf("text %q", res.Text)
		}
	})
	t.Run("two forms draw each other", func(t *testing.T) {
		f := imageDoc(t, "/A Do", func(b *pdftest.Builder) string {
			a, c := b.Reserve(), b.Reserve()
			b.Set(a, pdftest.Stream(fmt.Sprintf("/Type /XObject /Subtype /Form /Resources << /XObject << /B %d 0 R >> >>", c), "/B Do"))
			b.Set(c, pdftest.Stream(fmt.Sprintf("/Type /XObject /Subtype /Form /Resources << /XObject << /A %d 0 R >> >>", a), "/A Do"))
			return fmt.Sprintf("/XObject << /A %d 0 R >>", a)
		})
		extractAll(t, f, Options{Images: true})
	})
	t.Run("doubling tree", func(t *testing.T) {
		// 40 levels, each drawing the next twice: 2^40 forms if followed.
		f := imageDoc(t, "/L0 Do", func(b *pdftest.Builder) string {
			img := b.AddStream("/Type /XObject /Subtype /Image /Width 1 /Height 1", nil)
			next := img
			for i := 39; i >= 0; i-- {
				next = b.AddStream(fmt.Sprintf("/Type /XObject /Subtype /Form /Resources << /XObject << /N %d 0 R >> >>", next), []byte("/N Do /N Do"))
			}
			return fmt.Sprintf("/XObject << /L0 %d 0 R >>", next)
		})
		start := time.Now()
		res := extractAll(t, f, Options{Images: true})[0]
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("took %v", d)
		}
		if !strings.Contains(codes(res.Warnings), gofastcore.WarnLimitExceeded) {
			t.Errorf("warnings %q, want limit.exceeded", codes(res.Warnings))
		}
	})
}

// limitDoc has ten pages; the content of page 4 is larger than the
// stream limit and that of page 7 is exactly at it.
func limitDoc(t testing.TB) *file.File {
	const limit = 1 << 16
	pad := func(text string, size int) string {
		body := pdftest.TextPage(text)
		return body + strings.Repeat(" ", size-len(body))
	}
	pages := make([]pdftest.PageSpec, 10)
	for i := range pages {
		pages[i].Content = pdftest.TextPage(fmt.Sprintf("page %d", i+1))
	}
	pages[3].Content = pad("starts within the limit", limit) + pdftest.TextPage("beyond the limit")
	pages[6].Content = pad("exactly at the limit", limit)
	return open(t, pdftest.Doc(pdftest.DocOptions{Compress: true}, pages...).Bytes(), file.Limits{MaxStreamBytes: limit})
}

func TestStreamLimit(t *testing.T) {
	res := extractAll(t, limitDoc(t), Options{})
	if got := res[3]; got.Text != "starts within the limit" || codes(got.Warnings) != gofastcore.WarnLimitExceeded || got.Warnings[0].Severity != gofastcore.SeverityError {
		t.Errorf("page 4: text %q, warnings %+v", got.Text, got.Warnings)
	}
	if got := res[6]; got.Text != "exactly at the limit" || len(got.Warnings) != 0 {
		t.Errorf("page 7: text %q, warnings %s", got.Text, codes(got.Warnings))
	}
	if got := res[4]; got.Text != "page 5" || len(got.Warnings) != 0 {
		t.Errorf("the page after the cut one: %q, %s", got.Text, codes(got.Warnings))
	}
}

func TestPageBudget(t *testing.T) {
	// Three content streams of 40 KB each and a budget of 100 KB for the
	// page: the third is read only in part.
	b := pdftest.New()
	font := b.Add(pdftest.FontHelvetica)
	var refs []string
	for i := range 3 {
		body := pdftest.TextPage(fmt.Sprintf("stream %d", i+1))
		refs = append(refs, fmt.Sprintf("%d 0 R", b.AddFlate("", []byte(body+strings.Repeat(" ", 40000-len(body))))))
	}
	pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{})
	f := open(t, b.Bytes(), file.Limits{})
	page := f.Pages()[0]
	b.Set(int(page.Obj), fmt.Sprintf("<< /Type /Page /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> /Contents [%s] >>", font, strings.Join(refs, " ")))
	f = open(t, b.Bytes(), file.Limits{})
	res := Page(f, &f.Pages()[0], Options{MaxDecodedBytes: 100000})
	if res.Decoded != 100000 || !strings.Contains(res.Text, "stream 3") || codes(res.Warnings) != gofastcore.WarnLimitExceeded {
		t.Errorf("decoded %d, text %q, warnings %s", res.Decoded, res.Text, codes(res.Warnings))
	}
	res = Page(f, &f.Pages()[0], Options{MaxDecodedBytes: 80000})
	if res.Decoded != 80000 || strings.Contains(res.Text, "stream 3") || codes(res.Warnings) != gofastcore.WarnLimitExceeded {
		t.Errorf("budget 80000: decoded %d, text %q, warnings %s", res.Decoded, res.Text, codes(res.Warnings))
	}
}

// The same pages extracted from many goroutines in different orders give
// what a sequential run gives, limits and warnings included. Run with
// -race.
func TestConcurrentPagesAreDeterministic(t *testing.T) {
	for name, f := range map[string]*file.File{
		"with a limit": limitDoc(t),
		"fixture":      open(t, benchDoc(200), file.Limits{}),
	} {
		want := extractAll(t, f, Options{Images: true})
		pages := f.Pages()
		var wg sync.WaitGroup
		var mu sync.Mutex
		var diffs []string
		for g := range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for k := range pages {
					i := (k*7 + g*13) % len(pages)
					got := Page(f, &pages[i], Options{Images: true})
					if got.Text != want[i].Text || codes(got.Warnings) != codes(want[i].Warnings) || got.Decoded != want[i].Decoded || fmt.Sprint(got.Images) != fmt.Sprint(want[i].Images) {
						mu.Lock()
						diffs = append(diffs, fmt.Sprintf("page %d", i+1))
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
		if len(diffs) > 0 {
			t.Errorf("%s: %d results differ from the sequential run, first %s", name, len(diffs), diffs[0])
		}
	}
}

func benchDoc(n int) []byte {
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("Line %d of the page carries ordinary words for the extractor to read.", i))
	}
	pages := make([]pdftest.PageSpec, n)
	for i := range pages {
		pages[i].Content = pdftest.TextPage(lines...)
	}
	return pdftest.Doc(pdftest.DocOptions{Compress: true}, pages...).Bytes()
}

// Defect P1: a page of ordinary text costs a handful of allocations.
func TestAllocationsPerPage(t *testing.T) {
	if pdftest.RaceEnabled {
		t.Skip("allocation counts are not meaningful under the race detector")
	}
	f := open(t, benchDoc(50), file.Limits{})
	pages := f.Pages()
	run := func() {
		for i := range pages {
			Page(f, &pages[i], Options{})
		}
	}
	run() // fonts are parsed and buffers grown on the first pass
	if perPage := testing.AllocsPerRun(5, run) / float64(len(pages)); perPage > 6 {
		t.Errorf("%.1f allocations per page, budget 6", perPage)
	}
}

func FuzzExtract(f *testing.F) {
	f.Add(pdftest.OnePage(pdftest.TextPage("Hello, world")))
	f.Add(pdftest.Doc(pdftest.DocOptions{Compress: true, Configure: func(b *pdftest.Builder) { b.ObjStm = true }},
		pdftest.PageSpec{Content: "q 1 0 0 1 0 0 cm BT /F1 12 Tf [(a) -20 (b)] TJ ET Q BI /W 1 /H 1 ID x EI"}).Bytes())
	f.Add(bytes.Replace(pdftest.OnePage("BT /F1 1 Tf (x) Tj ET"), []byte("/Encoding /WinAnsiEncoding"),
		[]byte("/Encoding << /Differences [65 /B] >>"), 1))
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := file.Open(data, file.Limits{MaxStreamBytes: 1 << 20, MaxPages: 64})
		if err != nil {
			return
		}
		pages := file.Pages()
		for i := range pages {
			res := Page(file, &pages[i], Options{Images: true, MaxDecodedBytes: 4 << 20})
			if res.Err != nil && strings.Contains(res.Err.Error(), "internal error") {
				t.Fatalf("page %d: a panic was caught at the page boundary: %v", i+1, res.Err)
			}
			if !utf8.ValidString(res.Text) {
				t.Fatalf("page %d: text is not valid UTF-8", i+1)
			}
		}
	})
}
