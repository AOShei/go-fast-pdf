package content

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// nextRef is Next as it was before it read numbers itself and classed
// bytes with a table of its own: what Next must still do, byte for byte.
func (s *Scanner) nextRef() (op []byte, args []Operand, ok bool) {
	if s.done {
		s.args, s.elems, s.done = s.args[:0], s.elems[:0], false
	}
	data := s.data
	for {
		s.pos = syntax.SkipSpace(data, s.pos)
		if s.pos >= len(data) {
			return nil, nil, false
		}
		c := data[s.pos]
		if syntax.IsRegular(c) && !isNumberStart(c) {
			start := s.pos
			for s.pos < len(data) && syntax.IsRegular(data[s.pos]) {
				s.pos++
			}
			tok := data[start:s.pos]
			switch string(tok) {
			case "true", "false", "null":
				s.push(Operand{Kind: Other, Raw: tok})
				continue
			case "BI":
				s.inlineImage()
			}
			s.done = true
			return tok, s.args, true
		}
		if o, ok := s.operand(true); ok {
			s.push(o)
		}
	}
}

// trace writes what a scanner gives for the streams of a page, fed in
// turn: every operator with its operands and their elements, the
// inline images, and the counts of what was skipped and cut.
func trace(streams [][]byte, next func(*Scanner) ([]byte, []Operand, bool)) string {
	var b bytes.Buffer
	var s Scanner
	for i, data := range streams {
		if i == 0 {
			s.Reset(data)
		} else {
			s.Feed(data)
		}
		for n := 0; ; n++ {
			op, args, ok := next(&s)
			if !ok {
				break
			}
			if n > len(data)+1 {
				b.WriteString("more operators than bytes\n")
				break
			}
			fmt.Fprintf(&b, "%q:", op)
			for _, a := range args {
				fmt.Fprintf(&b, " %d%q", a.Kind, a.Raw)
				for _, e := range s.Elems(a) {
					fmt.Fprintf(&b, " [%d%q]", e.Kind, e.Raw)
				}
			}
			if string(op) == "BI" {
				fmt.Fprintf(&b, " dict %q data %q", s.InlineDict, s.InlineData)
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "end of stream %d at %d: skipped %d, cut %d\n", i, s.pos, s.Skipped, s.ElemsCut)
	}
	return b.String()
}

func sameAsBefore(t *testing.T, data []byte) {
	t.Helper()
	// The page whole, and the same bytes split over two streams.
	for _, streams := range [][][]byte{{data}, {data[:len(data)/2], data[len(data)/2:]}} {
		got := trace(streams, (*Scanner).Next)
		want := trace(streams, (*Scanner).nextRef)
		if got != want {
			t.Fatalf("%q in %d streams:\ngot\n%s\nwant\n%s", data, len(streams), got, want)
		}
	}
}

func TestSameAsBefore(t *testing.T) {
	for _, data := range []string{
		"",
		"q 1 0 0 1 72 720 cm BT /F1 12 Tf [(Hel) -20 (lo)] TJ ET Q 0 0 m 10 10 l S",
		"BI /W 2 /H 2 /BPC 8 /CS /G ID \x00\x01\x02\x03 EI BT (after image) Tj ET",
		"/Span << /ActualText (x) >> BDC [[1] <41> (a\\)b)] TJ EMC } ) >",
		"% a comment\r1 2 %another\n3 re true false null f",
		"+-.5 -.5. 1..2 ++ -- . 4.5e3 1e Tc",
		"[1 true 2 false x] TJ [ ( unterminated",
		"0 0 m\x00\x09\x0a\x0c\x0d\x20 1 1 l %\x80\xff\n S\x80\xff",
		"12 34 56", // operands and no operator
		pdftest.TextPage("Hello, world", "second line"),
	} {
		sameAsBefore(t, []byte(data))
	}
	// Every byte, alone and between others.
	for c := 0; c < 256; c++ {
		sameAsBefore(t, []byte{byte(c)})
		sameAsBefore(t, []byte{'1', byte(c), '2', ' ', 'T', 'j', byte(c), 'x'})
	}
}

func FuzzSameAsBefore(f *testing.F) {
	f.Add([]byte(pdftest.TextPage("Hello, world", "second line")))
	f.Add([]byte("q 1 0 0 1 72 720 cm BT /F1 12 Tf [(Hel) -20 (lo)] TJ ET Q 0 0 m 10 10 l S"))
	f.Add([]byte("BI /W 2 /H 2 /BPC 8 /CS /G ID \x00\x01\x02\x03 EI BT (after image) Tj ET"))
	f.Add([]byte("/Span << /ActualText (x) >> BDC [[1] <41> (a\\)b)] TJ EMC } ) >"))
	f.Add([]byte("% c\r+-.5 1..2 true [x 1 null] TJ"))
	f.Fuzz(func(t *testing.T, data []byte) {
		sameAsBefore(t, data)
	})
}
