package file

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

func open(t *testing.T, data []byte) *File {
	t.Helper()
	f, err := Open(data, Limits{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return f
}

// content returns the decoded content stream of page i (0-based).
func content(t *testing.T, f *File, i int) string {
	t.Helper()
	if i >= len(f.Pages()) {
		t.Fatalf("page %d of %d", i+1, len(f.Pages()))
	}
	// /Contents is one stream or an array of them.
	c := f.Resolve(f.Pages()[i].Dict().Get("Contents"))
	streams := []syntax.Value{c}
	if c.Kind == syntax.Array {
		streams = c.List
	}
	var out []byte
	for _, s := range streams {
		data, err := f.Decode(f.Resolve(s), nil, 0)
		if err != nil {
			t.Fatalf("page %d content: %v", i+1, err)
		}
		out = append(out, data...)
	}
	return string(out)
}

func doc(configure func(*pdftest.Builder), contents ...string) *pdftest.Builder {
	pages := make([]pdftest.PageSpec, len(contents))
	for i, c := range contents {
		pages[i].Content = c
	}
	return pdftest.Doc(pdftest.DocOptions{Configure: configure}, pages...)
}

func TestOpenLayouts(t *testing.T) {
	layouts := map[string]func(*pdftest.Builder){
		"classic":         nil,
		"xref stream":     func(b *pdftest.Builder) { b.XRefStream = true },
		"object streams":  func(b *pdftest.Builder) { b.ObjStm = true },
		"no type field":   func(b *pdftest.Builder) { b.XRefStream, b.XRefNoType = true, true },
		"raw xref":        func(b *pdftest.Builder) { b.XRefStream, b.XRefRaw = true, true },
		"19-byte entries": func(b *pdftest.Builder) { b.EntryEOL = "\n" },
		"21-byte entries": func(b *pdftest.Builder) { b.EntryEOL = " \r\n" },
	}
	for name, configure := range layouts {
		for _, compress := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/compress=%v", name, compress), func(t *testing.T) {
				b := pdftest.Doc(pdftest.DocOptions{Configure: configure, Compress: compress},
					pdftest.PageSpec{Content: "page one"}, pdftest.PageSpec{Content: "page two"})
				f := open(t, b.Bytes())
				if len(f.Pages()) != 2 {
					t.Fatalf("%d pages, want 2", len(f.Pages()))
				}
				if got := content(t, f, 0) + "|" + content(t, f, 1); got != "page one|page two" {
					t.Errorf("content %q", got)
				}
				if p := f.Pages()[1]; p.Number != 2 || !p.HasMediaBox || p.MediaBox != [4]float64{0, 0, 612, 792} || !p.Resources().IsDict() {
					t.Errorf("page 2 is %+v", p)
				}
				if w := f.Warnings(); len(w) != 0 {
					t.Errorf("warnings on a well-formed file: %v", w)
				}
			})
		}
	}
}

// B1: /Length as an indirect reference, for sizes on both sides of the
// buffer sizes the old reader used.
func TestIndirectLength(t *testing.T) {
	for _, size := range []int{0, 1, 4095, 4096, 4097, 65535, 65536, 65537, 300000} {
		for _, lengthFirst := range []bool{false, true} {
			body := strings.Repeat("x", size)
			// The length object comes before or after the stream in the file.
			b := pdftest.New()
			length := b.Reserve()
			if !lengthFirst {
				length = 1000
			}
			b.Set(length, fmt.Sprint(size))
			pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{})
			contentObj := RefNumber(open(t, b.Bytes()).Pages()[0].Dict().Get("Contents"))
			b.Set(int(contentObj), fmt.Sprintf("<< /Length %d 0 R >>\nstream\n%s\nendstream", length, body))
			f := open(t, b.Bytes())
			if got := content(t, f, 0); got != body {
				t.Errorf("size %d, length first %v: got %d bytes", size, lengthFirst, len(got))
			}
		}
	}
}

func TestRevisions(t *testing.T) {
	for _, stream := range []bool{false, true} {
		b := doc(func(b *pdftest.Builder) { b.XRefStream = stream }, "old", "second")
		f0 := open(t, b.Bytes())
		contentObj := RefNumber(f0.Pages()[0].Dict().Get("Contents"))
		b.Update()
		b.Set(int(contentObj), pdftest.Stream("", "new"))
		b.Update()
		extra := b.Add("(added in the third revision)")
		f := open(t, b.Bytes())
		if got := content(t, f, 0) + "|" + content(t, f, 1); got != "new|second" {
			t.Errorf("stream=%v: content %q, want the newest revision", stream, got)
		}
		if got := f.Object(int64(extra)); string(got.Data) != "added in the third revision" {
			t.Errorf("stream=%v: object from the last revision is %v", stream, got)
		}
	}
}

func TestMissingFreeAndCyclicObjects(t *testing.T) {
	b := doc(nil, "x")
	loop := b.Reserve()
	b.Set(loop, fmt.Sprintf("%d 0 R", loop))
	a, c := b.Reserve(), b.Reserve()
	b.Set(a, fmt.Sprintf("%d 0 R", c))
	b.Set(c, fmt.Sprintf("%d 0 R", a))
	freed := b.Add("(will be freed)")
	b.Update()
	b.Free(freed)
	f := open(t, b.Bytes())

	for name, v := range map[string]syntax.Value{
		"missing object":       f.Object(9999),
		"negative number":      f.Object(-1),
		"object 0":             f.Object(0),
		"freed object":         f.Object(int64(freed)),
		"self reference":       f.Resolve(syntax.Value{Kind: syntax.Ref, N: int64(loop)}),
		"two-object cycle":     f.Resolve(syntax.Value{Kind: syntax.Ref, N: int64(a)}),
		"reference to nothing": f.Resolve(syntax.Value{Kind: syntax.Ref, N: 123456}),
	} {
		if !v.IsNull() {
			t.Errorf("%s resolves to %v, want null", name, v)
		}
	}
}

func pageTree(t *testing.T, edit func(b *pdftest.Builder)) *File {
	t.Helper()
	b := doc(nil, "a", "b", "c")
	edit(b)
	return open(t, b.Bytes())
}

func TestPageTree(t *testing.T) {
	t.Run("inheritance over two levels", func(t *testing.T) {
		b := pdftest.New()
		catalog, top, mid, page := b.Reserve(), b.Reserve(), b.Reserve(), b.Reserve()
		font := b.Add(pdftest.FontHelvetica)
		b.Set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", top))
		b.Set(top, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 /MediaBox [0 0 100 200] /Rotate 90 /Resources << /Font << /F1 %d 0 R >> >> >>", mid, font))
		b.Set(mid, fmt.Sprintf("<< /Type /Pages /Parent %d 0 R /Kids [%d 0 R] /Count 1 /CropBox [10 10 90 190] >>", top, page))
		b.Set(page, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /Rotate -90 >>", mid))
		p := open(t, b.Bytes()).Pages()
		if len(p) != 1 {
			t.Fatalf("%d pages", len(p))
		}
		if p[0].MediaBox != [4]float64{0, 0, 100, 200} || p[0].CropBox != [4]float64{10, 10, 90, 190} || p[0].Rotate != 270 || !p[0].Resources().Get("Font").IsDict() {
			t.Errorf("page is %+v", p[0])
		}
	})
	t.Run("box corners in any order", func(t *testing.T) {
		f := pageTree(t, func(b *pdftest.Builder) {
			b.Set(2, "<< /Type /Pages /Kids [5 0 R] /Count 1 /MediaBox [612 792 0 0] >>")
			b.Set(5, "<< /Type /Page /Parent 2 0 R >>")
		})
		if p := f.Pages()[0]; p.MediaBox != [4]float64{0, 0, 612, 792} {
			t.Errorf("MediaBox %v", p.MediaBox)
		}
	})
	t.Run("no /Type anywhere", func(t *testing.T) {
		f := pageTree(t, func(b *pdftest.Builder) {
			b.Set(2, "<< /Kids [5 0 R 7 0 R] /Count 2 >>")
			b.Set(5, "<< /Parent 2 0 R /Contents 4 0 R >>")
			b.Set(7, "<< /Parent 2 0 R /Contents 6 0 R >>")
		})
		if len(f.Pages()) != 2 || content(t, f, 1) != "b" {
			t.Errorf("%d pages", len(f.Pages()))
		}
	})
	t.Run("node lists itself", func(t *testing.T) {
		f := pageTree(t, func(b *pdftest.Builder) { b.Set(2, "<< /Type /Pages /Kids [2 0 R 5 0 R 2 0 R 7 0 R] /Count 4 >>") })
		if len(f.Pages()) != 2 {
			t.Errorf("%d pages, want the 2 real ones", len(f.Pages()))
		}
	})
	t.Run("page listed twice", func(t *testing.T) {
		// Other readers show the page each time it is listed.
		f := pageTree(t, func(b *pdftest.Builder) { b.Set(2, "<< /Type /Pages /Kids [5 0 R 5 0 R 7 0 R] /Count 3 >>") })
		if len(f.Pages()) != 3 || content(t, f, 0) != "a" || content(t, f, 1) != "a" || content(t, f, 2) != "b" {
			t.Errorf("%d pages, want the first page twice and then the second", len(f.Pages()))
		}
	})
	t.Run("page count is bounded", func(t *testing.T) {
		kids := strings.Repeat("5 0 R ", 5000)
		b := doc(nil, "a")
		b.Set(2, "<< /Type /Pages /Kids ["+kids+"] /Count 5000 >>")
		f, err := Open(b.Bytes(), Limits{MaxPages: 100})
		if err != nil || len(f.Pages()) != 100 {
			t.Fatalf("%d pages, %v; want 100", len(f.Pages()), err)
		}
		if w := f.Warnings(); len(w) != 1 || w[0].Code != gofastcore.WarnLimitExceeded {
			t.Errorf("warnings %v", w)
		}
	})
	t.Run("wrong count", func(t *testing.T) {
		for _, count := range []string{"0", "1", "1000000000", "-5", "/Many"} {
			f := pageTree(t, func(b *pdftest.Builder) { b.Set(2, "<< /Type /Pages /Kids [5 0 R 7 0 R 9 0 R] /Count "+count+" >>") })
			if len(f.Pages()) != 3 {
				t.Errorf("/Count %s: %d pages, want 3", count, len(f.Pages()))
			}
		}
	})
	t.Run("entry that is not a page", func(t *testing.T) {
		f := pageTree(t, func(b *pdftest.Builder) { b.Set(7, "42") })
		p := f.Pages()
		if len(p) != 3 || p[0].Err != nil || p[1].Err == nil || p[2].Err != nil || p[2].Number != 3 {
			t.Fatalf("pages %+v: want three, the second failed", p)
		}
		if !errors.Is(p[1].Err, gofastcore.ErrMalformed) {
			t.Errorf("error %v does not wrap ErrMalformed", p[1].Err)
		}
	})
	t.Run("kids that are not an array", func(t *testing.T) {
		f := pageTree(t, func(b *pdftest.Builder) { b.Set(2, "<< /Type /Pages /Kids 5 /Count 1 >>") })
		if len(f.Pages()) != 0 {
			t.Errorf("%d pages", len(f.Pages()))
		}
	})
	t.Run("10,000 pages in a flat tree", func(t *testing.T) {
		contents := make([]string, 10000)
		start := time.Now()
		f := open(t, doc(nil, contents...).Bytes())
		if len(f.Pages()) != 10000 || f.Pages()[9999].Number != 10000 {
			t.Errorf("%d pages", len(f.Pages()))
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("building and opening took %v", d)
		}
	})
	t.Run("chain of 100,000 nested nodes", func(t *testing.T) {
		const depth = 100000
		b := pdftest.New()
		catalog := b.Reserve()
		nodes := make([]int, depth)
		for i := range nodes {
			nodes[i] = b.Reserve()
		}
		leaf := b.Add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R >>", nodes[depth-1]))
		b.Set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", nodes[0]))
		for i, n := range nodes {
			kid := leaf
			if i+1 < depth {
				kid = nodes[i+1]
			}
			b.Set(n, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 >>", kid))
		}
		if f := open(t, b.Bytes()); len(f.Pages()) != 1 {
			t.Errorf("%d pages", len(f.Pages()))
		}
	})
}

func TestOpenErrors(t *testing.T) {
	// Files with nothing in them to recover.
	hexCatalog := doc(nil, "x")
	hexCatalog.Set(1, "<feedbeef>")
	cases := map[string][]byte{
		"empty":               nil,
		"not a pdf":           []byte("hello world"),
		"header only":         []byte("%PDF-1.7\n%%EOF\n"),
		"catalog is a string": hexCatalog.Bytes(),
	}
	for name, data := range cases {
		_, err := Open(data, Limits{})
		if !errors.Is(err, gofastcore.ErrMalformed) {
			t.Errorf("%s: error %v, want ErrMalformed", name, err)
		}
	}
}

func TestStreamLimits(t *testing.T) {
	t.Run("inflate stops at the limit", func(t *testing.T) {
		big := bytes.Repeat([]byte("0123456789abcdef"), 1<<16) // 1 MiB
		b := pdftest.Doc(pdftest.DocOptions{Compress: true}, pdftest.PageSpec{Content: string(big)})
		f, err := Open(b.Bytes(), Limits{MaxStreamBytes: 100000})
		if err != nil {
			t.Fatal(err)
		}
		stm := f.Resolve(f.Pages()[0].Dict().Get("Contents"))
		data, err := f.Decode(stm, nil, 0)
		if !errors.Is(err, gofastcore.ErrLimitExceeded) || len(data) != 100000 || !bytes.Equal(data, big[:100000]) {
			t.Errorf("got %d bytes, error %v; want the first 100000 and ErrLimitExceeded", len(data), err)
		}
		// A stream of exactly the limit is not an error.
		data, err = f.Decode(stm, nil, 1<<20)
		if err != nil || len(data) != 1<<20 {
			t.Errorf("at the limit: %d bytes, error %v", len(data), err)
		}
		// Appending keeps what is already in the buffer.
		data, err = f.Decode(stm, []byte("head:"), 16)
		if !errors.Is(err, ErrTruncated) || string(data) != "head:0123456789abcdef" {
			t.Errorf("append: %q, %v", data, err)
		}
	})
	t.Run("allocation is bounded for a bomb", func(t *testing.T) {
		bomb := pdftest.Doc(pdftest.DocOptions{Compress: true}, pdftest.PageSpec{Content: string(make([]byte, 64<<20))}).Bytes()
		f, err := Open(bomb, Limits{MaxStreamBytes: 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		stm := f.Resolve(f.Pages()[0].Dict().Get("Contents"))
		n := pdftest.AllocBytes(func() { f.Decode(stm, nil, 0) })
		if n > 4<<20 {
			t.Errorf("allocated %d KB to refuse a stream with a 1 MB limit", n>>10)
		}
	})
	t.Run("length beyond the file", func(t *testing.T) {
		b := doc(nil, "")
		b.Set(4, "<< /Length 1000000000000 >>\nstream\nreal data\nendstream")
		f := open(t, b.Bytes())
		if got := content(t, f, 0); got != "real data" {
			t.Errorf("content %q, want the data up to endstream", got)
		}
	})
	t.Run("damaged flate keeps the prefix", func(t *testing.T) {
		text := strings.Repeat("some readable text. ", 2000)
		z := pdftest.Deflate([]byte(text))
		b := doc(nil, "")
		b.Set(4, pdftest.Stream("/Filter /FlateDecode", string(z[:len(z)/2])))
		f := open(t, b.Bytes())
		data, err := f.Decode(f.Resolve(f.Pages()[0].Dict().Get("Contents")), nil, 0)
		if err == nil || len(data) == 0 || !strings.HasPrefix(text, string(data)) {
			t.Errorf("got %d bytes and error %v; want a prefix of the text and an error", len(data), err)
		}
	})
	t.Run("unsupported filter", func(t *testing.T) {
		b := doc(nil, "")
		b.Set(4, pdftest.Stream("/Filter /JBIG2Decode", "xx"))
		f := open(t, b.Bytes())
		_, err := f.Decode(f.Resolve(f.Pages()[0].Dict().Get("Contents")), nil, 0)
		if !errors.Is(err, gofastcore.ErrUnsupported) {
			t.Errorf("error %v, want ErrUnsupported", err)
		}
	})
}

func TestPredictor(t *testing.T) {
	// Rows of four bytes, each PNG filter type once, checked against the
	// rows they encode.
	rows := [][]byte{{10, 20, 30, 40}, {11, 22, 33, 44}, {5, 6, 7, 8}, {200, 100, 50, 25}, {1, 2, 3, 250}}
	var enc []byte
	prev := make([]byte, 4)
	for i, row := range rows {
		tag := byte(i % 5)
		enc = append(enc, tag)
		for k, x := range row {
			var a, b, c byte
			if k > 0 {
				a, c = row[k-1], prev[k-1]
			}
			b = prev[k]
			switch tag {
			case 1:
				x -= a
			case 2:
				x -= b
			case 3:
				x -= byte((int(a) + int(b)) / 2)
			case 4:
				x -= paeth(a, b, c)
			}
			enc = append(enc, x)
		}
		prev = row
	}
	b := doc(nil, "")
	b.Set(4, pdftest.Stream("/Filter /FlateDecode /DecodeParms << /Predictor 12 /Columns 4 >>", string(pdftest.Deflate(enc))))
	f := open(t, b.Bytes())
	if got, want := content(t, f, 0), string(bytes.Join(rows, nil)); got != want {
		t.Errorf("got %v, want %v", []byte(got), []byte(want))
	}
}

// One File read from many goroutines gives the same result as a
// sequential read. Run with -race.
func TestConcurrentUse(t *testing.T) {
	contents := make([]string, 200)
	for i := range contents {
		contents[i] = fmt.Sprintf("content of page %d", i+1)
	}
	for _, objstm := range []bool{false, true} {
		data := pdftest.Doc(pdftest.DocOptions{Compress: true, Configure: func(b *pdftest.Builder) { b.ObjStm = objstm }},
			func() []pdftest.PageSpec {
				p := make([]pdftest.PageSpec, len(contents))
				for i := range p {
					p[i].Content = contents[i]
				}
				return p
			}()...).Bytes()
		f := open(t, data)
		var wg sync.WaitGroup
		errs := make(chan string, 64)
		for g := range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for k := range len(contents) {
					i := (k*7 + g*13) % len(contents) // a different order per goroutine
					stm := f.Resolve(f.Pages()[i].Dict().Get("Contents"))
					got, err := f.Decode(stm, nil, 0)
					if err != nil || string(got) != contents[i] {
						select {
						case errs <- fmt.Sprintf("page %d: %q, %v", i+1, got, err):
						default:
						}
						return
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			t.Errorf("objstm=%v: %s", objstm, e)
		}
	}
}

func FuzzOpen(f *testing.F) {
	f.Add(doc(nil, "BT (x) Tj ET").Bytes())
	f.Add(doc(func(b *pdftest.Builder) { b.ObjStm = true }, "BT (x) Tj ET").Bytes())
	f.Add(pdftest.Doc(pdftest.DocOptions{Compress: true, Configure: func(b *pdftest.Builder) { b.XRefStream = true }}, pdftest.PageSpec{Content: "q Q"}).Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := Open(data, Limits{MaxStreamBytes: 1 << 20})
		if err != nil {
			return
		}
		for _, p := range file.Pages() {
			file.Decode(file.Resolve(p.Dict().Get("Contents")), nil, 0)
			file.Resolve(p.Resources().Get("Font"))
		}
		file.Info()
		for n := range int64(min(len(file.xref), 64)) {
			file.Object(n)
		}
	})
}
