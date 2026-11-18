package content

import (
	"fmt"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

func showOperand(s *Scanner, o Operand) string {
	switch o.Kind {
	case Number:
		return fmt.Sprint(o.Float())
	case Name:
		return "/" + string(o.Raw)
	case String, HexString:
		return fmt.Sprintf("%q", o.AppendString(nil))
	case Dict:
		return "<<" + strings.TrimSpace(string(o.Raw)) + ">>"
	case Other:
		return "~" + string(o.Raw)
	case Array:
		var parts []string
		for _, e := range s.Elems(o) {
			parts = append(parts, showOperand(s, e))
		}
		return "[" + strings.Join(parts, " ") + "]"
	}
	return "?"
}

// scanAll renders every operation of the given streams, fed in order.
func scanAll(streams ...string) string {
	var s Scanner
	var out []string
	for i, src := range streams {
		if i == 0 {
			s.Reset([]byte(src))
		} else {
			s.Feed([]byte(src))
		}
		for {
			op, args, ok := s.Next()
			if !ok {
				break
			}
			parts := make([]string, len(args))
			for k, a := range args {
				parts[k] = showOperand(&s, a)
			}
			text := string(op) + "(" + strings.Join(parts, " ") + ")"
			if string(op) == "BI" {
				text += fmt.Sprintf("{%q}", s.InlineData)
			}
			out = append(out, text)
		}
	}
	return strings.Join(out, " ")
}

func TestScanner(t *testing.T) {
	table := []struct{ src, want string }{
		{"BT /F1 12 Tf ET", "BT() Tf(/F1 12) ET()"},
		{"1 0 0 1 72 720 Tm", "Tm(1 0 0 1 72 720)"},
		{"(Hello) Tj", `Tj("Hello")`},
		{`(a\(b\)c \\ \101) Tj`, `Tj("a(b)c \\ A")`},
		{"(nested (parens) here) Tj", `Tj("nested (parens) here")`},
		{"<48656c6C 6F> Tj", `Tj("Hello")`},
		{"[(Hel) -20 (lo) 15.5 <57>] TJ", `TJ(["Hel" -20 "lo" 15.5 "W"])`},
		{"[] TJ [ ] TJ", "TJ([]) TJ([])"},
		{"(x) ' 1 2 (y) \"", `'("x") "(1 2 "y")`},
		{"T* q Q", "T*() q() Q()"},
		{"-1.5 .5 +3 4. cm", "cm(-1.5 0.5 3 4)"},
		{"1-2 Td", "Td(1 -2)"},
		{"/Name#20x /A/B Do", "Do(/Name#20x /A /B)"},
		{"true false null BDC", "BDC(~true ~false ~null)"},
		{"/OC /MC0 BDC EMC", "BDC(/OC /MC0) EMC()"},
		{"/Span << /ActualText (a >> b) /X << /Y 1 >> >> BDC", "BDC(/Span <</ActualText (a >> b) /X << /Y 1 >>>>)"},
		{"/Span <</MCID 0>>BDC", "BDC(/Span <</MCID 0>>)"},
		{"[[1 2] (a)] TJ", `TJ([~[1 2] "a"])`},
		{"[1 true (a)] TJ", `TJ([1 ~true "a"])`},
		{"% a comment\n1 w % another\r2 w", "w(1) w(2)"},
		{"0 0 m 10 10 l S 1 2 3 4 re f", "m(0 0) l(10 10) S() re(1 2 3 4) f()"},

		// Damage.
		{"(ok) Tj } (kept) Tj", `Tj("ok") Tj("kept")`},
		{") ] > { } (x) Tj", `Tj("x")`},
		{"[(a) (b)", ""},
		{"[(a) (b) TJ", ""},
		{"(unterminated Tj", ""},
		{"<4865 Tj", ""},
		{"<< /A 1 BDC", ""},
		{"12abc", "abc(12)"},

		// Inline images.
		{"BI /W 2 /H 2 /BPC 8 /CS /G ID \x00\x01\x02\x03\nEI Q", "BI(/W 2 /H 2 /BPC 8 /CS /G){\"\\x00\\x01\\x02\\x03\\n\"} Q()"},
		{"q BI /W 1 ID\nabEIcd EI\xff\xfe\nEI\nQ", "q() BI(/W 1){\"abEIcd EI\\xff\\xfe\\n\"} Q()"},
		{"BI /W 1 ID xyz", "BI(/W 1){\"xyz\"}"},
		{"BI /W 1", "BI(/W 1){\"\"}"},
		{"1 2 3 BI /F [/A85 /Fl] ID\nx\nEI", "BI(/F [/A85 /Fl]){\"x\\n\"}"},
	}
	for _, c := range table {
		if got := scanAll(c.src); got != c.want {
			t.Errorf("%q:\n got  %s\n want %s", c.src, got, c.want)
		}
	}
}

// Operands carry over from one content stream of a page to the next.
func TestFeedKeepsOperands(t *testing.T) {
	if got, want := scanAll("BT /F1 12", " Tf (a", ") Tj ET"), `BT() Tf(/F1 12) Tj("a") ET()`; got == want {
		t.Log("a string split between streams also survived, which the format does not require")
	}
	if got, want := scanAll("BT /F1 12", " Tf 1 0 0", " 1 5 6 Tm ET"), "BT() Tf(/F1 12) Tm(1 0 0 1 5 6) ET()"; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	// Reset drops them.
	var s Scanner
	s.Reset([]byte("1 2 3"))
	s.Next()
	s.Reset([]byte("Td"))
	if _, args, _ := s.Next(); len(args) != 0 {
		t.Errorf("Reset kept %d operands", len(args))
	}
}

func TestOperandFlood(t *testing.T) {
	// A million numbers and no operator: the stack must not grow with them.
	var s Scanner
	s.Reset([]byte(strings.Repeat("1 ", 1_000_000) + "Td"))
	_, args, ok := s.Next()
	if !ok || len(args) > maxOperands || cap(s.args) > 4*maxOperands {
		t.Errorf("ok=%v, %d operands, capacity %d", ok, len(args), cap(s.args))
	}
}

// Scanning allocates nothing once the operand stack has grown, and path
// operands in particular are never converted (defect P2).
func TestScanAllocatesNothing(t *testing.T) {
	var sb strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&sb, "%d.5 %d m %d %d.25 l 1 2 3 4 5 6 c h S 0.5 w 1 0 0 rg 10 20 30 40 re f\n", i, i, i, i)
		sb.WriteString("BT /F1 10 Tf 72 700 Td [(Some) -20 (text)] TJ ET\n")
	}
	data := []byte(sb.String())
	var s Scanner
	run := func() {
		s.Reset(data)
		for {
			if _, _, ok := s.Next(); !ok {
				break
			}
		}
	}
	run() // grow the stack once
	if allocs := testing.AllocsPerRun(10, run); allocs != 0 {
		t.Errorf("%v allocations per scan of %d KB", allocs, len(data)>>10)
	}
}

func BenchmarkScanner(b *testing.B) {
	var sb strings.Builder
	for i := 0; sb.Len() < 1<<20; i++ {
		fmt.Fprintf(&sb, "BT /F1 10 Tf 72 %d Td [(Line %d of the) -250 (benchmark) 30 (page)] TJ ET\n", 720-i%60*12, i)
		sb.WriteString("0.5 w 72 100 m 540 100 l S 72.5 101.25 468 12 re f q 1 0 0 1 10 10 cm Q\n")
	}
	data := []byte(sb.String())
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	var s Scanner
	for b.Loop() {
		s.Reset(data)
		for {
			if _, _, ok := s.Next(); !ok {
				break
			}
		}
	}
}

func FuzzScanner(f *testing.F) {
	f.Add([]byte(pdftest.TextPage("Hello, world", "second line")))
	f.Add([]byte("q 1 0 0 1 72 720 cm BT /F1 12 Tf [(Hel) -20 (lo)] TJ ET Q 0 0 m 10 10 l S"))
	f.Add([]byte("BI /W 2 /H 2 /BPC 8 /CS /G ID \x00\x01\x02\x03 EI BT (after image) Tj ET"))
	f.Add([]byte("/Span << /ActualText (x) >> BDC [[1] <41> (a\\)b)] TJ EMC } ) >"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var s Scanner
		s.Reset(data)
		// Every call consumes at least one byte, so the number of
		// operators is bounded by the length of the input.
		for n := 0; ; n++ {
			op, args, ok := s.Next()
			if !ok {
				break
			}
			if n > len(data) {
				t.Fatalf("more operators than bytes")
			}
			for _, a := range args {
				a.Float()
				a.AppendString(nil)
				s.Elems(a)
			}
			_ = op
		}
	})
}
