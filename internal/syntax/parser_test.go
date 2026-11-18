package syntax

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// show renders a value in a compact form for comparison in tests.
func show(v Value) string {
	switch v.Kind {
	case Null:
		return "null"
	case Bool:
		return fmt.Sprint(v.N == 1)
	case Int:
		return fmt.Sprintf("%d", v.N)
	case Real:
		return fmt.Sprintf("%g", v.F)
	case Name:
		return "/" + string(v.Data)
	case String:
		if v.Hex {
			return fmt.Sprintf("<%q>", v.Data)
		}
		return fmt.Sprintf("(%q)", v.Data)
	case Ref:
		return fmt.Sprintf("%d %d R", v.N, v.Gen)
	case Keyword:
		return "kw:" + string(v.Data)
	case Array:
		parts := make([]string, len(v.List))
		for i, e := range v.List {
			parts[i] = show(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case Dict, Stream:
		parts := make([]string, 0, len(v.List)/2)
		for i := 0; i+1 < len(v.List); i += 2 {
			parts = append(parts, show(v.List[i])+" "+show(v.List[i+1]))
		}
		return "<<" + strings.Join(parts, " ") + ">>"
	}
	return "?"
}

// parseAll reads every object in src.
func parseAll(src string) (string, error) {
	p := NewParser([]byte(src), 0, 0)
	var parts []string
	for {
		v, err := p.Object()
		if err == io.EOF {
			return strings.Join(parts, " | "), nil
		}
		if err != nil {
			return strings.Join(parts, " | "), err
		}
		parts = append(parts, show(v))
	}
}

func TestObjects(t *testing.T) {
	cases := []struct{ src, want string }{
		// Scalars.
		{"null true false", "null | true | false"},
		{"nulls truex", "kw:nulls | kw:truex"},
		{"0 12 -7 +3 007", "0 | 12 | -7 | 3 | 7"},
		{"1.5 -2.25 .5 -.5 +.5 4. 0.0", "1.5 | -2.25 | 0.5 | -0.5 | 0.5 | 4 | 0"},
		{"- . + --1 -+2", "0 | 0 | 0 | -1 | -2"},
		{"1-2 3.5.6 12abc", "1 | -2 | 3.5 | 0.6 | 12 | kw:abc"},
		{"9223372036854775807 -9223372036854775807", "9223372036854775807 | -9223372036854775807"},
		{"0.1 0.000001 123456.789", "0.1 | 1e-06 | 123456.789"},

		// Names.
		{"/Name /A#20B /#2F /", "/Name | /A B | // | /"},
		{"/a#zz /a#4 /lime#20Green", "/a#zz | /a#4 | /lime Green"},
		{"/A/B/C", "/A | /B | /C"},
		{"/Name(str)", `/Name | ("str")`},

		// Literal strings.
		{"(plain)", `("plain")`},
		{"()", `("")`},
		{"(a (nested (deep)) b)", `("a (nested (deep)) b")`},
		{`(\(\)\\)`, `("()\\")`},
		{`(\n\r\t\b\f\q)`, `("\n\r\t\b\fq")`},
		{`(\101\102\1039 \0 \12x \777)`, `("ABC9 \x00 \nx \xff")`},
		{"(line\\\ncontinued)", `("linecontinued)`[:15] + `")`},
		{"(a\\\r\nb)", `("ab")`},
		{"(a\r\nb\rc\nd)", `("a\nb\nc\nd")`},
		{"(unterminated", `("unterminated")`},
		{"(ends with backslash\\", `("ends with backslash")`},

		// Hexadecimal strings.
		{"<48656C6C6F>", `<"Hello">`},
		{"<48 65\n6c 6C 6f>", `<"Hello">`},
		{"<4>", `<"@">`},
		{"<>", `<"">`},
		{"<4z8>", `<"H">`},
		{"<48", `<"H">`},

		// References.
		{"12 0 R", "12 0 R"},
		{"12 0 R 7", "12 0 R | 7"},
		{"12 0 Rx", "12 | 0 | kw:Rx"},
		{"12 0 obj", "12 | 0 | kw:obj"},
		{"1 2 3 R", "1 | 2 3 R"},
		{"-1 0 R", "-1 | 0 | kw:R"},
		{"1.5 0 R", "1.5 | 0 | kw:R"},
		{"12 0\nR", "12 0 R"},
		{"[1 0 R 2 0 R]", "[1 0 R 2 0 R]"},
		{"12 0 R/Name", "12 0 R | /Name"},

		// Arrays and dictionaries.
		{"[]", "[]"},
		{"[1 /A (s) [2 3] <<>>]", `[1 /A ("s") [2 3] <<>>]`},
		{"<< /A 1 /B [2] /C << /D (x) >> >>", `<</A 1 /B [2] /C <</D ("x")>>>>`},
		{"<</A/B/C 1>>", "<</A /B /C 1>>"},
		{"<< /A >>", "<</A null>>"},
		{"<< /A 1 /B >>", "<</A 1 /B null>>"},
		{"<</A 1 0 R/B 2 0 R>>", "<</A 1 0 R /B 2 0 R>>"},

		// Damage inside containers is skipped.
		{"[1 ) 2 } 3]", "[1 2 3]"},
		{"[1 2", "[1 2]"},
		{"<< /A 1 /B", "<</A 1 /B null>>"},
		{"<< 5 /A 1 >>", "<</A 1>>"},
		{"<< /A 1 > /B 2", "<</A 1>> | /B | 2"},

		// Comments and white space.
		{"1 % comment\n2", "1 | 2"},
		{"1 % comment\r2", "1 | 2"},
		{"%only a comment", ""},
		{"\x00\t\f 1", "1"},
		{"(a % not a comment)", `("a % not a comment")`},

		// Keywords.
		{"obj endobj stream BT ET Tj ' \" T*", `kw:obj | kw:endobj | kw:stream | kw:BT | kw:ET | kw:Tj | kw:' | kw:" | kw:T*`},
	}
	for _, c := range cases {
		got, err := parseAll(c.src)
		if err != nil {
			t.Errorf("%q: error %v after %q", c.src, err, got)
			continue
		}
		if got != c.want {
			t.Errorf("%q:\n got  %s\n want %s", c.src, got, c.want)
		}
	}
}

func TestIntegerOverflowBecomesReal(t *testing.T) {
	p := NewParser([]byte("99999999999999999999 -99999999999999999999"), 0, 0)
	for _, want := range []float64{1e20, -1e20} {
		v, err := p.Object()
		if err != nil || v.Kind != Real || v.F/want < 0.999999 || v.F/want > 1.000001 {
			t.Errorf("got %s (%v), want a real near %g", show(v), err, want)
		}
	}
}

func TestSyntaxErrorDoesNotAdvance(t *testing.T) {
	for _, src := range []string{")", "]", ">", "}", "{"} {
		p := NewParser([]byte(src+" 1"), 0, 0)
		if _, err := p.Object(); err != ErrSyntax {
			t.Errorf("%q: error %v, want ErrSyntax", src, err)
		}
		if p.Pos() != 0 {
			t.Errorf("%q: position moved to %d", src, p.Pos())
		}
	}
}

func TestDepthLimit(t *testing.T) {
	deep := strings.Repeat("[", 1_000_000)
	p := NewParser([]byte(deep), 0, 0)
	if _, err := p.Object(); err != ErrTooDeep {
		t.Errorf("a million nested arrays: error %v, want ErrTooDeep", err)
	}
	p = NewParser([]byte(strings.Repeat("<</A ", 200)+"1"), 0, 0)
	if _, err := p.Object(); err != ErrTooDeep {
		t.Errorf("200 nested dictionaries: error %v, want ErrTooDeep", err)
	}
	ok := strings.Repeat("[", 50) + "1" + strings.Repeat("]", 50)
	p = NewParser([]byte(ok), 0, 0)
	if _, err := p.Object(); err != nil {
		t.Errorf("50 nested arrays: %v", err)
	}
	p = NewParser([]byte("[[[1]]]"), 0, 2)
	if _, err := p.Object(); err != ErrTooDeep {
		t.Errorf("limit 2, depth 3: error %v, want ErrTooDeep", err)
	}
}

func TestValueAccess(t *testing.T) {
	p := NewParser([]byte("<< /Type /Page /Count 3 /Size 4.0 /Kids [1 0 R] /Type /Second >>"), 0, 0)
	v, err := p.Object()
	if err != nil {
		t.Fatal(err)
	}
	if !v.Get("Type").IsName("Page") {
		t.Errorf("Get(Type) = %s, want the first /Page", show(v.Get("Type")))
	}
	if n, ok := v.Get("Count").Integer(); !ok || n != 3 {
		t.Errorf("Count = %d, %v", n, ok)
	}
	if n, ok := v.Get("Size").Integer(); !ok || n != 4 {
		t.Errorf("a real with no fraction should read as an integer: %d, %v", n, ok)
	}
	if !v.Get("Missing").IsNull() || v.Has("Missing") || !v.Has("Kids") {
		t.Error("Get or Has is wrong for a missing or present key")
	}
	if len(v.Get("Kids").Elems()) != 1 || v.Entries() != 5 {
		t.Errorf("Kids has %d elements, the dictionary %d entries", len(v.Get("Kids").Elems()), v.Entries())
	}
	if _, ok := v.Get("Type").Integer(); ok {
		t.Error("a name read as an integer")
	}
}

func TestStreamData(t *testing.T) {
	cases := []struct {
		src  string
		want int
		ok   bool
	}{
		{"stream\ndata", 7, true},
		{"stream\r\ndata", 8, true},
		{"stream\rdata", 7, true},
		{"  \n stream\ndata", 11, true},
		{"streams\n", 0, false},
		{"endobj", 0, false},
		{"stream", 6, true},
	}
	for _, c := range cases {
		got, ok := StreamData([]byte(c.src), 0)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %d, %v; want %d, %v", c.src, got, ok, c.want, c.ok)
		}
	}
}

// Names and strings without escapes must point into the input.
func TestNoCopyForPlainTokens(t *testing.T) {
	src := []byte("/Name (string) keyword 12 0 R 1.5 true")
	allocs := testing.AllocsPerRun(100, func() {
		p := NewParser(src, 0, 0)
		for {
			if _, err := p.Object(); err != nil {
				break
			}
		}
	})
	if allocs != 0 {
		t.Errorf("%v allocations for tokens that need none", allocs)
	}
}

func FuzzParser(f *testing.F) {
	f.Add([]byte("<< /Type /Page /Kids [1 0 R 2 0 R] /Name (text \\(nested\\)) /Hex <48656C6C6F> >>"))
	f.Add([]byte("[1 -2 +3.5 .25 4. true false null /Name#20with#20space]"))
	f.Add([]byte("12 0 obj\n(string) endobj"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p := NewParser(data, 0, 0)
		for {
			before := p.Pos()
			_, err := p.Object()
			if err == io.EOF {
				return
			}
			if err != nil {
				if err == ErrSyntax && (p.Pos() >= len(data) || !IsDelim(data[p.Pos()])) {
					t.Fatalf("ErrSyntax left the position at %d, which is not the offending byte", p.Pos())
				}
				p.SetPos(p.Pos() + 1)
				continue
			}
			if p.Pos() <= before {
				t.Fatalf("no progress at %d", before)
			}
			if p.Pos() > len(data) {
				t.Fatalf("position %d is past the end (%d)", p.Pos(), len(data))
			}
		}
	})
}
