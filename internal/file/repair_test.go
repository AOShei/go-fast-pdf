package file

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

func repaired(f *File) bool {
	for _, w := range f.Warnings() {
		if w.Code == gofastcore.WarnInputRepaired {
			return true
		}
	}
	return false
}

func TestScanObjects(t *testing.T) {
	src := "%PDF-1.7\n" +
		"1 0 obj\n<< /A 1 >>\nendobj\n" + // offset 9
		"12 3 obj<</B 2>>endobj\n" +
		"  7   0   obj\r(obj) endobj\n" +
		"x5 0 obj not an object: glued to a letter\n" +
		"5 0 object not an object: the keyword goes on\n" +
		"5 0obj no space before the keyword\n" +
		"0 0 obj object number zero is never used\n" +
		"99999999999 0 obj too many digits\n" +
		"3 0 obj\n"
	got := scanObjects([]byte(src), 1<<20)
	var parts []string
	for _, o := range got {
		parts = append(parts, fmt.Sprintf("%d/%d@%d", o.num, o.gen, o.off))
	}
	want := fmt.Sprintf("1/0@9 12/3@%d 7/0@%d 3/0@%d", strings.Index(src, "12 3 obj"), strings.Index(src, "7   0"), strings.LastIndex(src, "3 0 obj"))
	if strings.Join(parts, " ") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(parts, " "), want)
	}
	if n := len(scanObjects([]byte(src), 10)); n != 3 {
		t.Errorf("with a limit of 10 objects: %d found, want the 3 below it", n)
	}
}

// cut returns data up to the last occurrence of marker.
func cut(data []byte, marker string) []byte {
	return append([]byte{}, data[:bytes.LastIndex(data, []byte(marker))]...)
}

// The cross-reference data as a whole is unusable: the file opens from
// its objects, says so, and reads the same.
func TestRebuild(t *testing.T) {
	layouts := map[string]func(*pdftest.Builder){
		"classic":        nil,
		"xref stream":    func(b *pdftest.Builder) { b.XRefStream = true },
		"object streams": func(b *pdftest.Builder) { b.ObjStm = true },
	}
	damages := map[string]func([]byte) []byte{
		"startxref missing": func(d []byte) []byte { return cut(d, "startxref") },
		"startxref points nowhere": func(d []byte) []byte {
			return append(cut(d, "startxref"), "startxref\n7\n%%EOF\n"...)
		},
		"startxref beyond the file": func(d []byte) []byte {
			return append(cut(d, "startxref"), "startxref\n99999999\n%%EOF\n"...)
		},
		"startxref without a number": func(d []byte) []byte {
			return append(cut(d, "startxref"), "startxref\n%%EOF\n"...)
		},
		"last 40 bytes cut off": func(d []byte) []byte { return append([]byte{}, d[:len(d)-40]...) },
	}
	for lname, configure := range layouts {
		for dname, damage := range damages {
			t.Run(lname+"/"+dname, func(t *testing.T) {
				for _, compress := range []bool{false, true} {
					good := pdftest.Doc(pdftest.DocOptions{Configure: configure, Compress: compress},
						pdftest.PageSpec{Content: "page one"}, pdftest.PageSpec{Content: "page two"}).Bytes()
					f, err := Open(damage(good), Limits{})
					if err != nil {
						t.Fatalf("compress=%v: %v", compress, err)
					}
					if len(f.Pages()) != 2 || content(t, f, 0) != "page one" || content(t, f, 1) != "page two" {
						t.Errorf("compress=%v: %d pages", compress, len(f.Pages()))
					}
					if !repaired(f) {
						t.Errorf("compress=%v: no input.repaired warning: %v", compress, f.Warnings())
					}
					if !f.Resources(0).Get("Font").IsDict() {
						t.Errorf("compress=%v: the font resources are lost", compress)
					}
				}
			})
		}
	}

	t.Run("no trailer at all", func(t *testing.T) {
		// Cut before the table: the catalog is found by its /Type.
		good := doc(nil, "kept").Bytes()
		f := open(t, cut(good, "\nxref\n"))
		if len(f.Pages()) != 1 || content(t, f, 0) != "kept" || !repaired(f) {
			t.Errorf("%d pages, warnings %v", len(f.Pages()), f.Warnings())
		}
	})
	t.Run("catalog inside an object stream, no cross-reference stream", func(t *testing.T) {
		good := doc(func(b *pdftest.Builder) { b.ObjStm = true }, "kept").Bytes()
		// Remove the cross-reference stream object and everything after.
		at := bytes.LastIndex(good, []byte("/Type /XRef"))
		at = bytes.LastIndex(good[:at], []byte(" 0 obj"))
		at = bytes.LastIndexByte(good[:at], '\n') + 1
		f := open(t, good[:at])
		if len(f.Pages()) != 1 || content(t, f, 0) != "kept" {
			t.Errorf("%d pages", len(f.Pages()))
		}
	})
	t.Run("update cut off after its objects", func(t *testing.T) {
		// The second revision lost its cross-reference section. The first
		// revision's is intact and is what a reader sees.
		b := doc(nil, "old")
		contentObj := RefNumber(open(t, b.Bytes()).Pages()[0].Dict.Get("Contents"))
		b.Update()
		b.Set(int(contentObj), pdftest.Stream("", "new"))
		f := open(t, cut(b.Bytes(), "startxref"))
		if got := content(t, f, 0); got != "old" || repaired(f) {
			t.Errorf("content %q, warnings %v", got, f.Warnings())
		}
	})
	t.Run("the newest revision wins", func(t *testing.T) {
		for _, objstm := range []bool{false, true} {
			b := doc(func(b *pdftest.Builder) { b.ObjStm = objstm }, "old")
			contentObj := RefNumber(open(t, b.Bytes()).Pages()[0].Dict.Get("Contents"))
			b.Update()
			b.Set(int(contentObj), pdftest.Stream("", "new"))
			info := b.Add("<< /Title (second revision) >>")
			b.Trailer = fmt.Sprintf("/Info %d 0 R", info)
			// Every startxref is damaged, the older revision's too:
			// otherwise that revision's table would simply be used.
			f := open(t, bytes.ReplaceAll(b.Bytes(), []byte("startxref"), []byte("startxxxx")))
			if got := content(t, f, 0); got != "new" {
				t.Errorf("objstm=%v: content %q, want the newer revision", objstm, got)
			}
			if !objstm && string(f.Info().Get("Title").Data) != "second revision" {
				t.Errorf("objstm=%v: the newer trailer's /Info is lost", objstm)
			}
		}
	})
	t.Run("trailer names an object that is not the catalog", func(t *testing.T) {
		b := doc(nil, "kept")
		b.Set(40, "42")
		b.Root = 40
		f := open(t, b.Bytes())
		if len(f.Pages()) != 1 || content(t, f, 0) != "kept" || !repaired(f) {
			t.Errorf("%d pages, warnings %v", len(f.Pages()), f.Warnings())
		}
	})
	t.Run("encrypted file keeps its key", func(t *testing.T) {
		good, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "enc-aes-128.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		want := content(t, open(t, good), 0)
		// Only startxref is damaged: the trailer, with /Encrypt and /ID,
		// is still in the file.
		bad := append(cut(good, "startxref"), "startxref\n1\n%%EOF\n"...)
		f := open(t, bad)
		if !f.IsEncrypted() || content(t, f, 0) != want {
			t.Error("content differs from the undamaged file")
		}
	})
}

// Resources returns the resource dictionary of page i.
func (f *File) Resources(i int) syntax.Value { return f.Pages()[i].Resources }

// The table is usable but wrong about some objects: those are found by
// scanning, the rest come from the table.
func TestObjectsTheTableGetsWrong(t *testing.T) {
	t.Run("wrong offset", func(t *testing.T) {
		data := doc(nil, "x").Bytes()
		f := open(t, data)
		want := f.Object(3)
		f = open(t, data)
		f.xref[3].off = f.xref[4].off // the table says the font is where the content stream is
		if got := f.Object(3); !got.Get("BaseFont").IsName("Helvetica") || len(got.List) != len(want.List) {
			t.Errorf("object 3 is %v", got)
		}
		if !repaired(f) {
			t.Errorf("warnings %v", f.Warnings())
		}
	})
	t.Run("unreadable entry", func(t *testing.T) {
		data := doc(nil, "kept").Bytes()
		table := bytes.Index(data, []byte("\nxref\n")) + 1
		entry := table + len("xref\n0 6\n") + 20*3 // the entry of object 3
		bad := append([]byte{}, data...)
		copy(bad[entry:], "00000/0377")
		f := open(t, bad)
		if len(f.Pages()) != 1 || content(t, f, 0) != "kept" || !f.Object(3).Get("BaseFont").IsName("Helvetica") {
			t.Errorf("%d pages, object 3 is %v", len(f.Pages()), f.Object(3))
		}
	})
	t.Run("bytes inserted in the middle", func(t *testing.T) {
		// Everything after the second object has moved by 11 bytes.
		data := doc(nil, "first", "second").Bytes()
		at := bytes.Index(data, []byte("3 0 obj"))
		bad := append(append(append([]byte{}, data[:at]...), "% inserted\n"...), data[at:]...)
		f := open(t, bad)
		if len(f.Pages()) != 2 || content(t, f, 0) != "first" || content(t, f, 1) != "second" || !repaired(f) {
			t.Errorf("%d pages, warnings %v", len(f.Pages()), f.Warnings())
		}
	})
	t.Run("object not in the table", func(t *testing.T) {
		// The table covers objects 0 to 5; object 9 exists in the file.
		b := doc(nil, "x")
		data := b.Bytes()
		at := bytes.Index(data, []byte("\nxref\n")) + 1
		bad := append(append(append([]byte{}, data[:at]...), "9 0 obj\n(found by scanning)\nendobj\n"...), data[at:]...)
		// The table has moved; fix startxref so that only object 9 is at issue.
		old := fmt.Sprintf("startxref\n%d\n", at)
		bad = bytes.Replace(bad, []byte(old), []byte(fmt.Sprintf("startxref\n%d\n", at+len("9 0 obj\n(found by scanning)\nendobj\n"))), 1)
		f := open(t, bad)
		if got := f.Object(9); string(got.Data) != "found by scanning" {
			t.Errorf("object 9 is %v", got)
		}
		if got := f.Object(10); !got.IsNull() {
			t.Errorf("object 10 does not exist and is %v", got)
		}
	})
	t.Run("a deleted object stays deleted", func(t *testing.T) {
		b := doc(nil, "x")
		gone := b.Add("(deleted in the second revision)")
		b.Update()
		b.Free(gone)
		f := open(t, b.Bytes())
		f.Object(9999) // trigger the scan
		if got := f.Object(int64(gone)); !got.IsNull() {
			t.Errorf("a freed object came back: %v", got)
		}
	})
	t.Run("same answer in any order", func(t *testing.T) {
		data := doc(nil, "first", "second", "third").Bytes()
		at := bytes.Index(data, []byte("3 0 obj"))
		bad := append(append(append([]byte{}, data[:at]...), "% inserted\n"...), data[at:]...)
		want := map[int64]string{}
		f := open(t, bad)
		for n := int64(1); n < 12; n++ {
			want[n] = fmt.Sprint(f.Object(n))
		}
		for round := range 20 {
			f := open(t, bad)
			var wg sync.WaitGroup
			var mu sync.Mutex
			for g := range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for k := range int64(11) {
						n := (k*int64(3+g)+int64(round))%11 + 1
						if got := fmt.Sprint(f.Object(n)); got != want[n] {
							mu.Lock()
							t.Errorf("object %d differs when read concurrently", n)
							mu.Unlock()
						}
					}
				}()
			}
			wg.Wait()
		}
	})
}

func TestHeaderOffset(t *testing.T) {
	good := doc(nil, "after the junk").Bytes()
	junk := "HTTP/1.1 200 OK\r\nContent-Type: application/pdf\r\n\r\n"
	t.Run("offsets counted from the header", func(t *testing.T) {
		f := open(t, doc(func(b *pdftest.Builder) { b.Prefix = []byte(junk) }, "after the junk").Bytes())
		if content(t, f, 0) != "after the junk" || len(f.Warnings()) != 0 {
			t.Errorf("warnings %v", f.Warnings())
		}
	})
	t.Run("offsets counted from the start of the file", func(t *testing.T) {
		// Junk, then a file whose offsets include the junk: build the
		// file with a comment as long as the junk in front of object 1,
		// then swap the comment and the header.
		b := doc(nil, "after the junk")
		header := "%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"
		b.Version = "1.7\n%\xe2\xe3\xcf\xd3\n%" + strings.Repeat("j", len(junk)-2)
		built := b.Bytes()
		comment := "%" + strings.Repeat("j", len(junk)-2) + "\n"
		at := bytes.Index(built, []byte(comment))
		if at != len(header) {
			t.Fatalf("the comment is at %d, expected %d", at, len(header))
		}
		file := append([]byte(comment), built[:at]...) // junk first, then the header
		file = append(file, built[at+len(comment):]...)
		f := open(t, file)
		if content(t, f, 0) != "after the junk" || len(f.Warnings()) != 0 {
			t.Errorf("warnings %v", f.Warnings())
		}
	})
	t.Run("junk longer than a kilobyte", func(t *testing.T) {
		// No header within reach: the offsets are all wrong and the file
		// is read by scanning.
		bad := append(bytes.Repeat([]byte("junk "), 400), good...)
		f := open(t, bad)
		if content(t, f, 0) != "after the junk" || !repaired(f) {
			t.Errorf("warnings %v", f.Warnings())
		}
	})
	t.Run("bytes after the end", func(t *testing.T) {
		// 4 KB appended after %%EOF: startxref is no longer near the end.
		f := open(t, append(append([]byte{}, good...), bytes.Repeat([]byte("\x00"), 4096)...))
		if content(t, f, 0) != "after the junk" || len(f.Warnings()) != 0 {
			t.Errorf("warnings %v", f.Warnings())
		}
	})
}

func TestHybridFile(t *testing.T) {
	// The catalog, page tree, font and page are in an object stream that
	// only the stream named by /XRefStm lists.
	build := func(tableListsHiddenAsFree bool) []byte {
		var out bytes.Buffer
		out.WriteString("%PDF-1.5\n")
		offs := map[int]int{}
		write := func(n int, body string) {
			offs[n] = out.Len()
			fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
		}
		packed := []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [5 0 R] /Count 1 >>",
			pdftest.FontHelvetica,
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents 4 0 R >>",
		}
		nums := []int{1, 2, 3, 5}
		var index, bodies strings.Builder
		var rows []byte
		for i, body := range packed {
			fmt.Fprintf(&index, "%d %d ", nums[i], bodies.Len())
			bodies.WriteString(body + "\n")
			rows = append(rows, 2, 0, 0, 0, 6, 0, byte(i))
		}
		write(4, pdftest.Stream("", "hybrid"))
		write(6, pdftest.Stream(fmt.Sprintf("/Type /ObjStm /N 4 /First %d", index.Len()), index.String()+bodies.String()))
		write(7, pdftest.Stream("/Type /XRef /Size 8 /W [1 4 2] /Index [1 3 5 1] /Root 1 0 R", string(rows)))
		xref := out.Len()
		if tableListsHiddenAsFree {
			out.WriteString("xref\n0 8\n")
			for n := 0; n < 8; n++ {
				switch off, ok := offs[n]; {
				case ok:
					fmt.Fprintf(&out, "%010d %05d n \n", off, 0)
				case n == 0:
					out.WriteString("0000000000 65535 f \n")
				default:
					out.WriteString("0000000000 00000 f \n")
				}
			}
		} else {
			out.WriteString("xref\n0 1\n0000000000 65535 f \n")
			fmt.Fprintf(&out, "4 1\n%010d %05d n \n", offs[4], 0)
			fmt.Fprintf(&out, "6 2\n%010d %05d n \n%010d %05d n \n", offs[6], 0, offs[7], 0)
		}
		fmt.Fprintf(&out, "trailer\n<< /Size 8 /Root 1 0 R /XRefStm %d >>\nstartxref\n%d\n%%%%EOF\n", offs[7], xref)
		return out.Bytes()
	}
	for _, asFree := range []bool{false, true} {
		f := open(t, build(asFree))
		if len(f.Pages()) != 1 || content(t, f, 0) != "hybrid" || len(f.Warnings()) != 0 {
			t.Errorf("hidden objects listed as free=%v: %d pages, warnings %v", asFree, len(f.Pages()), f.Warnings())
		}
	}
}

func TestWrongStreamLength(t *testing.T) {
	const body = "the real stream data"
	for name, length := range map[string]string{
		"zero": "0", "too short": "7", "too long": "45", "one too long": fmt.Sprint(len(body) + 1),
		"far beyond the file": "1000000000000", "negative": "-5", "a name": "/Long", "missing": "",
	} {
		dict := "<< >>"
		if length != "" {
			dict = "<< /Length " + length + " >>"
		}
		b := doc(nil, "", "the page after it")
		b.Set(4, dict+"\nstream\n"+body+"\nendstream")
		f := open(t, b.Bytes())
		// A length that is one too long takes in the line end before
		// endstream, which is harmless.
		if got := strings.TrimRight(content(t, f, 0), "\n"); got != body {
			t.Errorf("/Length %s: %q", name, got)
		}
		if got := content(t, f, 1); got != "the page after it" {
			t.Errorf("/Length %s: the next stream reads %q", name, got)
		}
	}
	// A right length is trusted even when the data contains the keyword.
	tricky := "data with endstream inside it"
	b := doc(nil, "")
	b.Set(4, pdftest.Stream("", tricky))
	if got := content(t, open(t, b.Bytes()), 0); got != tricky {
		t.Errorf("a correct /Length was not trusted: %q", got)
	}
}

func FuzzRebuild(f *testing.F) {
	f.Add(cut(doc(nil, "BT (x) Tj ET").Bytes(), "startxref"))
	f.Add(cut(doc(func(b *pdftest.Builder) { b.ObjStm = true }, "q Q").Bytes(), "startxref"))
	f.Add([]byte("1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj 2 0 obj<</Kids[3 0 R]>>endobj 3 0 obj<</Contents 4 0 R>>endobj 4 0 obj<</Length 3>>stream\nq Q\nendstream"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Without startxref every input goes down the rebuilding path.
		data = bytes.ReplaceAll(data, []byte("startxref"), []byte("startxxxx"))
		file, err := Open(data, Limits{MaxStreamBytes: 1 << 20, MaxPages: 64})
		if err != nil {
			return
		}
		for _, p := range file.Pages() {
			file.Decode(file.Resolve(p.Dict.Get("Contents")), nil, 0)
		}
		for n := range int64(40) {
			file.Object(n)
		}
	})
}

// The table grows in proportion to the highest object number, in
// whatever order the numbers arrive. Two earlier versions of the growing
// code reallocated on every call.
func TestXRefGrowthIsLinear(t *testing.T) {
	for name, order := range map[string]func(i int) int64{
		"ascending":  func(i int) int64 { return int64(i + 1) },
		"descending": func(i int) int64 { return int64(100000 - i) },
		"scattered":  func(i int) int64 { return int64(i*7919%100000 + 1) },
	} {
		for _, put := range []bool{false, true} {
			f := &File{limits: DefaultLimits()}
			n := pdftest.AllocBytes(func() {
				for i := range 100000 {
					if put {
						f.putEntry(order(i), entry{kind: entryOffset})
					} else {
						f.setEntry(order(i), entry{kind: entryOffset})
					}
				}
			})
			// 100,000 entries of 24 bytes are 2.4 MB; doubling may
			// allocate a few times that in total.
			if n > 16<<20 {
				t.Errorf("%s, put=%v: %d MB allocated for 100,000 entries", name, put, n>>20)
			}
			if len(f.xref) != 100001 {
				t.Errorf("%s, put=%v: table has %d entries", name, put, len(f.xref))
			}
		}
	}
}
