package pdf

import (
	"bytes"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// Fuzz targets for the parsers. In Phase 0 they are stubs: the seeds are
// well-formed inputs, which run as ordinary tests, and fuzzing itself
// starts in Phase 1, because v0.1.1 has known crashes and hangs on
// malformed input (defects C1 to C4) that a fuzzer finds in seconds. The
// targets that CI fuzzes are listed in .github/fuzz-targets.txt.
//
// Every loop is bounded by the input length so that a parser that fails
// to make progress shows up as a slow input, not as a fuzzer that hangs.

func FuzzLexer(f *testing.F) {
	f.Add([]byte("<< /Type /Page /Kids [1 0 R 2 0 R] /Count 2 /Name (text \\(nested\\)) /Hex <48656C6C6F> >>"))
	f.Add([]byte("[1 -2 +3.5 .25 4. true false null /Name#20with#20space]"))
	f.Add([]byte("12 0 obj\n(string) endobj"))
	f.Fuzz(func(t *testing.T, data []byte) {
		l := NewLexer(bytes.NewReader(data))
		for range len(data) + 1 {
			if _, err := l.ReadObject(); err != nil {
				return
			}
		}
	})
}

func FuzzContent(f *testing.F) {
	f.Add([]byte(pdftest.TextPage("Hello, world", "second line")))
	f.Add([]byte("q 1 0 0 1 72 720 cm BT /F1 12 Tf [(Hel) -20 (lo)] TJ ET Q 0 0 m 10 10 l S"))
	f.Add([]byte("BI /W 2 /H 2 /BPC 8 /CS /G ID \x00\x01\x02\x03 EI BT (after image) Tj ET"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p := NewContentStreamParser(data)
		for range len(data) + 1 {
			if _, err := p.Next(); err != nil {
				return
			}
		}
	})
}

func FuzzCMap(f *testing.F) {
	f.Add([]byte("1 begincodespacerange <0000> <FFFF> endcodespacerange\n2 beginbfchar <0001> <0041> <0002> <00480069> endbfchar"))
	f.Add([]byte("1 beginbfrange <0010> <0020> <0041> endbfrange\n1 beginbfrange <0030> <0031> [<0061> <0062>] endbfrange"))
	f.Fuzz(func(t *testing.T, data []byte) {
		ParseCMap(data)
	})
}

func FuzzXRef(f *testing.F) {
	f.Add(pdftest.OnePage(pdftest.TextPage("classic table")))
	f.Add(pdftest.Doc(pdftest.DocOptions{Configure: func(b *pdftest.Builder) { b.ObjStm = true }},
		pdftest.PageSpec{Content: pdftest.TextPage("xref stream")}).Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		ParseXRef(bytes.NewReader(data))
	})
}
