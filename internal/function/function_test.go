package function

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// load makes a file of the objects add puts in it, and reads the
// function of the one it returns, its samples and program up to limit
// bytes; with a limit of 0 it is only checked.
func load(t *testing.T, limit int64, add func(b *pdftest.Builder) int) (*Func, int64, error) {
	t.Helper()
	b := pdftest.New()
	n := add(b)
	data := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{}).Bytes()
	f, err := file.Open(data, file.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	v := syntax.Value{Kind: syntax.Ref, N: int64(n)}
	if limit == 0 {
		fn, err := NewReader(f).Check(v)
		return fn, 0, err
	}
	return NewReader(f).Read(v, limit)
}

// one parses one object.
func one(t *testing.T, obj string) (*Func, error) {
	t.Helper()
	fn, _, err := load(t, 1<<20, func(b *pdftest.Builder) int { return b.Add(obj) })
	return fn, err
}

func calc(t *testing.T, dict, prog string) (*Func, error) {
	t.Helper()
	return one(t, pdftest.Stream("/FunctionType 4 "+dict, prog))
}

// eval evaluates fn and compares its outputs with want.
func eval(t *testing.T, fn *Func, in []float64, want ...float64) {
	t.Helper()
	out := make([]float64, fn.Out())
	fn.Eval(in, out)
	if len(out) != len(want) {
		t.Fatalf("%v: %v, want %v", in, out, want)
	}
	for i := range out {
		if math.Abs(out[i]-want[i]) > 1e-6 { // samples are kept as float32
			t.Errorf("%v: %v, want %v", in, out, want)
			return
		}
	}
}

// must takes a function parsed, failing the test on an error.
func must(t *testing.T) func(*Func, error) *Func {
	return func(fn *Func, err error) *Func {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return fn
	}
}

func TestExponential(t *testing.T) {
	fn := must(t)(one(t, "<< /FunctionType 2 /Domain [0 1] /C0 [0 1 0] /C1 [1 0 0.5] /N 2 >>"))
	if fn.In() != 1 || fn.Out() != 3 {
		t.Fatalf("%d in, %d out", fn.In(), fn.Out())
	}
	eval(t, fn, []float64{0.5}, 0.25, 0.75, 0.125)
	eval(t, fn, []float64{2}, 1, 0, 0.5) // clipped to the Domain
	// C0 and C1 by default, a Range clipping.
	fn = must(t)(one(t, "<< /FunctionType 2 /Domain [0 4] /Range [0 0.5] /N 1 >>"))
	eval(t, fn, []float64{0.25}, 0.25)
	eval(t, fn, []float64{3}, 0.5)
	// What cannot be computed is 0.
	fn = must(t)(one(t, "<< /FunctionType 2 /Domain [-1 1] /N 0.5 >>"))
	eval(t, fn, []float64{-1}, 0)
	for _, bad := range []string{
		"<< /FunctionType 2 /Domain [0 1] >>",
		"<< /FunctionType 2 /Domain [0 1 0 1] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /N 1 /C0 [0 0] /C1 [1] >>",
		"<< /FunctionType 2 /Domain [0 1] /N 1 /C0 [0 0] /C1 [1 1] /Range [0 1] >>",
		"<< /FunctionType 2 /Domain [0 1] /N 1 /C0 (x) >>",
		"<< /FunctionType 2 /Domain [0 1] /N 1 /C1 (x) >>",
	} {
		if _, err := one(t, bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

func TestStitching(t *testing.T) {
	fn, _, err := load(t, 1<<20, func(b *pdftest.Builder) int {
		up := b.Add("<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>")
		return b.Add(fmt.Sprintf("<< /FunctionType 3 /Domain [0 2] /Functions [%d 0 R << /FunctionType 2 /Domain [0 1] /C0 [10] /C1 [20] /N 1 >>] /Bounds [1] /Encode [0 1 1 0] >>", up))
	})
	must(t)(fn, err)
	eval(t, fn, []float64{0.5}, 0.5)
	eval(t, fn, []float64{1}, 20) // a bound starts the next subdomain
	eval(t, fn, []float64{1.5}, 15)
	eval(t, fn, []float64{2}, 10)
	eval(t, fn, []float64{-1}, 0)
	// A first subdomain of no width holds the bottom of the Domain.
	fn = must(t)(one(t, "<< /FunctionType 3 /Domain [0 1] /Functions [<< /FunctionType 2 /Domain [0 1] /C0 [7] /C1 [7] /N 1 >> << /FunctionType 2 /Domain [0 1] /N 1 >>] /Bounds [0] /Encode [0 1 0 1] >>"))
	eval(t, fn, []float64{0}, 7)
	eval(t, fn, []float64{0.5}, 0.5)
	// One function, no Bounds.
	fn = must(t)(one(t, "<< /FunctionType 3 /Domain [0 1] /Functions [<< /FunctionType 2 /Domain [0 1] /N 1 >>] /Bounds [] /Encode [1 0] >>"))
	eval(t, fn, []float64{0.25}, 0.75)

	f2 := "<< /FunctionType 2 /Domain [0 1] /N 1 >>"
	for _, bad := range []string{
		"<< /FunctionType 3 /Domain [0 1] /Functions [] /Bounds [] /Encode [] >>",
		"<< /FunctionType 3 /Domain [0 1] /Functions [" + f2 + " " + f2 + "] /Bounds [] /Encode [0 1 0 1] >>",
		"<< /FunctionType 3 /Domain [0 1] /Functions [" + f2 + " " + f2 + "] /Bounds [2] /Encode [0 1 0 1] >>",
		"<< /FunctionType 3 /Domain [0 1] /Functions [" + f2 + " " + f2 + "] /Bounds [0.5] /Encode [0 1] >>",
		"<< /FunctionType 3 /Domain [0 1] /Functions [" + f2 + " << /FunctionType 2 /Domain [0 1] /N 1 /C0 [0 0] /C1 [1 1] >>] /Bounds [0.5] /Encode [0 1 0 1] >>",
		"<< /FunctionType 3 /Domain [0 1 0 1] /Functions [" + f2 + "] /Bounds [] /Encode [0 1] >>",
		"<< /FunctionType 3 /Domain [0 1] /Range [0 1 0 1] /Functions [" + f2 + "] /Bounds [] /Encode [0 1] >>",
		"<< /FunctionType 3 /Domain [0 1] /Functions [<< /FunctionType 9 /Domain [0 1] >>] /Bounds [] /Encode [0 1] >>",
	} {
		if _, err := one(t, bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	// A function made of itself ends.
	_, _, err = load(t, 1<<20, func(b *pdftest.Builder) int {
		n := b.Reserve()
		b.Set(n, fmt.Sprintf("<< /FunctionType 3 /Domain [0 1] /Functions [%d 0 R] /Bounds [] /Encode [0 1] >>", n))
		return n
	})
	if err == nil {
		t.Error("a function of itself")
	}
}

// samples packs values of bits each, most significant first.
func samples(bits int, vs ...uint64) string {
	var out []byte
	var acc uint64
	n := 0
	for _, v := range vs {
		for i := bits - 1; i >= 0; i-- {
			acc = acc<<1 | v>>i&1
			if n++; n == 8 {
				out = append(out, byte(acc))
				acc, n = 0, 0
			}
		}
	}
	if n > 0 {
		out = append(out, byte(acc<<(8-n)))
	}
	return string(out)
}

func TestSampled(t *testing.T) {
	// One input, three outputs: black to orange.
	fn := must(t)(one(t, pdftest.Stream("/FunctionType 0 /Domain [0 1] /Range [0 1 0 1 0 1] /Size [2] /BitsPerSample 8", samples(8, 0, 0, 0, 255, 128, 0))))
	eval(t, fn, []float64{0}, 0, 0, 0)
	eval(t, fn, []float64{1}, 1, 128.0/255, 0)
	eval(t, fn, []float64{0.5}, 0.5, 64.0/255, 0)
	// Two inputs, bilinear; the first input varies fastest.
	fn = must(t)(one(t, pdftest.Stream("/FunctionType 0 /Domain [0 1 0 1] /Range [0 4] /Size [2 2] /BitsPerSample 4 /Decode [0 15]", samples(4, 0, 1, 2, 3))))
	eval(t, fn, []float64{0, 0}, 0)
	eval(t, fn, []float64{1, 0}, 1)
	eval(t, fn, []float64{0, 1}, 2)
	eval(t, fn, []float64{1, 1}, 3)
	eval(t, fn, []float64{0.5, 0.5}, 1.5)
	eval(t, fn, []float64{2, -1}, 1)
	// Encode picks part of the table, wide samples.
	for _, bps := range []int{12, 16, 24, 32} {
		top := uint64(1)<<bps - 1
		fn = must(t)(one(t, pdftest.Stream(fmt.Sprintf("/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [3] /BitsPerSample %d /Encode [1 2] /Order 3", bps), samples(bps, 0, top, top/2))))
		eval(t, fn, []float64{0}, 1)
		eval(t, fn, []float64{1}, float64(top/2)/float64(top))
	}
	for _, bits := range []int{1, 2} {
		fn = must(t)(one(t, pdftest.Stream(fmt.Sprintf("/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [2] /BitsPerSample %d", bits), samples(bits, 1<<bits-1, 0))))
		eval(t, fn, []float64{0.25}, 0.75)
	}

	// Only checked, its samples not read: the bytes decoded are none, and
	// it gives zeros.
	fn, decoded, err := load(t, 0, func(b *pdftest.Builder) int {
		return b.Add(pdftest.Stream("/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [2] /BitsPerSample 8", "\xFF\xFF"))
	})
	must(t)(fn, err)
	if decoded != 0 {
		t.Errorf("checked: %d bytes decoded", decoded)
	}
	eval(t, fn, []float64{1}, 0)
	// Its samples beyond the limit.
	_, _, err = load(t, 1, func(b *pdftest.Builder) int {
		return b.Add(pdftest.Stream("/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [2] /BitsPerSample 8", "\xFF\xFF"))
	})
	if !errors.Is(err, file.ErrTruncated) {
		t.Errorf("beyond the limit: %v", err)
	}

	const ok = "/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [2] /BitsPerSample 8"
	for _, bad := range []string{
		"<< " + ok + " >>",
		pdftest.Stream("/FunctionType 0 /Domain [0 1] /Size [2] /BitsPerSample 8", "ab"),
		pdftest.Stream(ok, "a"),
		pdftest.Stream(strings.Replace(ok, "8", "7", 1), "ab"),
		pdftest.Stream(strings.Replace(ok, "[2]", "[2 2]", 1), "ab"),
		pdftest.Stream(strings.Replace(ok, "[2]", "[0]", 1), "ab"),
		pdftest.Stream(strings.Replace(ok, "[2]", "[99999999]", 1), "ab"),
		pdftest.Stream(ok+" /Encode [0]", "ab"),
		pdftest.Stream(ok+" /Decode [0 1 0 1]", "ab"),
		pdftest.Stream("/FunctionType 0 /Domain [0 1 0 1 0 1 0 1 0 1 0 1 0 1 0 1 0 1 0 1 0 1 0 1 0 1] /Range [0 1] /Size [1 1 1 1 1 1 1 1 1 1 1 1 1] /BitsPerSample 8", "a"),
	} {
		if _, err := one(t, bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestArrayOfFunctions(t *testing.T) {
	fn := must(t)(one(t, "[<< /FunctionType 2 /Domain [0 1] /N 1 >> << /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>]"))
	if fn.In() != 1 || fn.Out() != 2 {
		t.Fatalf("%d in, %d out", fn.In(), fn.Out())
	}
	eval(t, fn, []float64{0.25}, 0.25, 0.75)
	for _, bad := range []string{
		"[]",
		"[<< /FunctionType 2 /Domain [0 1] /N 1 /C0 [0 0] /C1 [1 1] >>]",
		"[5]",
		"(x)",
		"<< /Domain [0 1] >>",
		"<< /FunctionType 2 /Domain [1 0] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /Range [1 0] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 (x)] /N 1 >>",
	} {
		if _, err := one(t, bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

func TestCalculator(t *testing.T) {
	r1 := "/Domain [-1000 1000 -1000 1000] /Range [-1000000000 1000000000]"
	for _, c := range []struct {
		prog string
		in   []float64
		want float64
	}{
		{"{ add }", []float64{2, 3}, 5},
		{"{ sub }", []float64{2, 3}, -1},
		{"{ mul }", []float64{2, 3}, 6},
		{"{ div }", []float64{3, 2}, 1.5},
		{"{ idiv }", []float64{-7, 2}, -3},
		{"{ mod }", []float64{-7, 2}, -1},
		{"{ exp }", []float64{2, 10}, 1024},
		{"{ atan }", []float64{0, -1}, 180},
		{"{ atan }", []float64{-1, 0}, 270},
		{"{ pop sin }", []float64{30, 0}, 0.5},
		{"{ pop cos }", []float64{60, 0}, 0.5},
		{"{ pop abs neg }", []float64{-4, 0}, -4},
		{"{ pop 2.5 add ceiling }", []float64{0, 0}, 3},
		{"{ pop -2.5 floor }", []float64{0, 0}, -3},
		{"{ pop -2.5 round }", []float64{0, 0}, -2},
		{"{ pop -2.5 truncate cvr }", []float64{0, 0}, -2},
		{"{ pop -2.7 cvi }", []float64{0, 0}, -2},
		{"{ pop 16 sqrt }", []float64{0, 0}, 4},
		{"{ pop 100 log 1 ln add }", []float64{0, 0}, 2},
		{"{ pop 1 2 bitshift 64 -3 bitshift add }", []float64{0, 0}, 12},
		{"{ pop 1 40 bitshift }", []float64{0, 0}, 0},
		{"{ and }", []float64{12, 10}, 8},
		{"{ or }", []float64{12, 10}, 14},
		{"{ xor }", []float64{12, 10}, 6},
		{"{ pop not }", []float64{5, 0}, -6},
		{"{ gt { 1 } { 2 } ifelse }", []float64{3, 2}, 1},
		{"{ gt { 1 } { 2 } ifelse }", []float64{2, 3}, 2},
		{"{ exch pop dup 5 lt { 7 add } if }", []float64{0, 3}, 10},
		{"{ exch pop dup 5 lt { 7 add } if }", []float64{0, 6}, 6},
		{"{ 2 copy le 3 1 roll ge and { 9 } { 8 } ifelse }", []float64{4, 4}, 9},
		{"{ 2 copy eq exch pop exch pop true and false or { 1 } { 0 } ifelse }", []float64{4, 4}, 1},
		{"{ ne true xor { 1 } { 0 } ifelse }", []float64{1, 2}, 0},
		{"{ 1 index }", []float64{5, 6}, 5},
		{"{ exch pop dup mul }", []float64{5, 6}, 36},
		{"{ 3 3 1 roll }", []float64{1, 2}, 2},           // 1 2 3 -> 3 1 2
		{"{ 3 3 -1 roll }", []float64{1, 2}, 1},          // 1 2 3 -> 2 3 1
		{"{ 0 0 roll pop }", []float64{1, 2}, 1},         // nothing rolled
		{"{ 1 3 { pop pop 1 } if }", []float64{0, 2}, 1}, // nested if, a number as true
		{"{ ge { 1 { 2 } { 3 } ifelse } { 4 } ifelse } % a comment\n", []float64{2, 1}, 2},
		{"{ add 1.5e1 add }", []float64{0, 0}, 15},
		// What fails gives 0.
		{"{ pop 0 div }", []float64{1, 0}, 0},
		{"{ 0 idiv }", []float64{1, 0}, 0},
		{"{ pop -1 sqrt }", []float64{1, 0}, 0},
		{"{ pop 0 ln }", []float64{1, 0}, 0},
		{"{ pop pop pop }", []float64{1, 0}, 0},
		{"{ pop pop }", []float64{1, 0}, 0},
		{"{ pop pop 0 0 atan }", []float64{1, 0}, 0},
		{"{ 5 index }", []float64{1, 0}, 0},
		{"{ 5 copy }", []float64{1, 0}, 0},
		{"{ 5 1 roll }", []float64{1, 0}, 0},
		{"{ 99 copy 99 copy }", []float64{1, 0}, 0},
		{"{ " + strings.Repeat("dup ", 120) + "}", []float64{1, 0}, 0},
		{"{ pop 1e300 dup mul }", []float64{1, 0}, 0},
	} {
		fn, err := calc(t, r1, c.prog)
		if err != nil {
			t.Errorf("%s: %v", c.prog, err)
			continue
		}
		out := []float64{math.NaN()}
		fn.Eval(c.in, out)
		if math.Abs(out[0]-c.want) > 1e-9 {
			t.Errorf("%s of %v: %v, want %v", c.prog, c.in, out[0], c.want)
		}
	}
	// Several outputs: the last on the stack, in order; the Range clips.
	fn := must(t)(calc(t, "/Domain [0 1] /Range [0 1 0 1 0 0.5]", "{ dup 1 exch sub 1 }"))
	eval(t, fn, []float64{0.75}, 0.75, 0.25, 0.5)
	eval(t, fn, []float64{2}, 1, 0, 0.5)

	for _, bad := range []string{
		"add",
		"{ add",
		"{ add } add",
		"{ { 1 } }",
		"{ { 1 } add }",
		"{ { 1 } { 2 } { 3 } ifelse }",
		"{ foo }",
		"{ 16#FF }",
		"{ 1.2.3 }",
		"{ " + strings.Repeat("{ ", 101) + "}",
		"{ " + strings.Repeat("1 ", maxProgram+1) + "}",
	} {
		if _, err := calc(t, r1, bad); err == nil {
			t.Errorf("%.40q: no error", bad)
		}
	}
	for _, bad := range []string{"<< /FunctionType 4 /Domain [0 1] /Range [0 1] >>", "<< /FunctionType 4 /Domain [0 1] >>"} {
		if _, err := one(t, bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	if _, err := one(t, pdftest.Stream("/FunctionType 4 /Domain [0 1] /Range [0 1] /Filter /FlateDecode", "not flate")); err == nil {
		t.Error("a damaged program: no error")
	}
	if fn, err := calc(t, r1, "{ foo\x01 }"); err == nil || strings.ContainsRune(err.Error(), 1) {
		t.Errorf("%v %v", fn, err)
	}
}

// A program counts against the limit; past it the function is cut, and a
// program longer than any is not one. Checked, it is not decoded.
func TestCalculatorLimit(t *testing.T) {
	prog := "{ " + strings.Repeat("1 pop ", 100) + "}"
	add := func(b *pdftest.Builder) int {
		return b.Add(pdftest.Stream("/FunctionType 4 /Domain [0 1] /Range [0 1]", prog))
	}
	fn, decoded, err := load(t, int64(len(prog)), add)
	must(t)(fn, err)
	if decoded != int64(len(prog)) {
		t.Errorf("%d bytes decoded, the program is %d", decoded, len(prog))
	}
	if _, _, err := load(t, int64(len(prog))-1, add); !errors.Is(err, file.ErrTruncated) {
		t.Errorf("a limit short of the program: %v", err)
	}
	long := "{ " + strings.Repeat(" ", 16*maxProgram) + "}"
	_, _, err = load(t, 1<<30, func(b *pdftest.Builder) int {
		return b.Add(pdftest.Stream("/FunctionType 4 /Domain [0 1] /Range [0 1]", long))
	})
	if err == nil || errors.Is(err, file.ErrTruncated) {
		t.Errorf("a program longer than any: %v", err)
	}
	// Checked: nothing is decoded, a damaged program is not seen, and
	// the function gives zeros.
	fn, decoded, err = load(t, 0, func(b *pdftest.Builder) int {
		return b.Add(pdftest.Stream("/FunctionType 4 /Domain [0 1] /Range [0 1] /Filter /FlateDecode", "not flate"))
	})
	must(t)(fn, err)
	if decoded != 0 {
		t.Errorf("checked: %d bytes decoded", decoded)
	}
	eval(t, fn, []float64{1}, 0)
}

// open makes a file of the objects add puts in it, and returns a Reader
// of it and the reference add returns.
func open(t *testing.T, add func(b *pdftest.Builder) int) (*Reader, syntax.Value) {
	t.Helper()
	b := pdftest.New()
	n := add(b)
	f, err := file.Open(pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{}).Bytes(), file.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return NewReader(f), syntax.Value{Kind: syntax.Ref, N: int64(n)}
}

// fan adds a function of depth levels of stitching functions, each of
// which stitches k times the same function of the level below.
func fan(b *pdftest.Builder, k, depth int) int {
	n := b.Add("<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>")
	bounds := make([]string, k-1)
	for i := range bounds {
		bounds[i] = fmt.Sprintf("%.6f", float64(i+1)/float64(k))
	}
	for range depth {
		n = b.Add(fmt.Sprintf("<< /FunctionType 3 /Domain [0 1] /Functions [%s] /Bounds [%s] /Encode [%s] >>",
			strings.Repeat(fmt.Sprintf("%d 0 R ", n), k), strings.Join(bounds, " "), strings.Repeat("0 1 ", k)))
	}
	return n
}

// A Reader reads a function that is an object once: a function that
// refers many times to another, at every level, is read in the work of
// its objects, not of its references; and one asked for again is the
// same, nothing decoded again.
func TestReaderReadsOnce(t *testing.T) {
	r, v := open(t, func(b *pdftest.Builder) int { return fan(b, 1024, maxDepth) })
	allocs := testing.AllocsPerRun(1, func() {
		fn, err := r.Check(v)
		must(t)(fn, err)
		fn, _, err = r.Read(v, 1<<20)
		must(t)(fn, err)
		eval(t, fn, []float64{0}, 0)
		eval(t, fn, []float64{1}, 1)
	})
	if allocs > 50000 {
		t.Errorf("%.0f allocations", allocs)
	}
	if spent := maxWork - r.work; spent > 2*(maxDepth+1)*3*1024 {
		t.Errorf("%d work for %d objects", spent, maxDepth+1)
	}

	r, v = open(t, func(b *pdftest.Builder) int {
		return b.Add(pdftest.Stream("/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [2] /BitsPerSample 8", "\x00\xFF"))
	})
	first, decoded, err := r.Read(v, 1<<20)
	must(t)(first, err)
	again, decoded2, err := r.Read(v, 1<<20)
	must(t)(again, err)
	if again != first || decoded != 2 || decoded2 != 0 {
		t.Errorf("read again: the same %v, %d and %d bytes decoded", again == first, decoded, decoded2)
	}
	// Past what a Reader keeps, a function is read again.
	r, v = open(t, func(b *pdftest.Builder) int {
		return b.Add(pdftest.Stream("/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [2] /BitsPerSample 8", "\x00\xFF"))
	})
	r.kept = maxKept
	first, _, err = r.Read(v, 1<<20)
	must(t)(first, err)
	if again, decoded, _ := r.Read(v, 1<<20); again == first || decoded != 2 {
		t.Errorf("past what is kept: the same %v, %d bytes decoded", again == first, decoded)
	}
	// Cut short by the limit, it is read in full under a larger one.
	r, v = open(t, func(b *pdftest.Builder) int {
		return b.Add(pdftest.Stream("/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [2] /BitsPerSample 8", "\x00\xFF"))
	})
	if _, _, err := r.Read(v, 1); !errors.Is(err, file.ErrTruncated) {
		t.Errorf("limit 1: %v", err)
	}
	must(t)(func() (*Func, error) { fn, _, err := r.Read(v, 2); return fn, err }())
}

// A function kept is nested no deeper where it is referred to again than
// a function read there for the first time would be.
func TestReaderDepth(t *testing.T) {
	var deepest int
	r, v := open(t, func(b *pdftest.Builder) int {
		deepest = fan(b, 1, maxDepth)
		return b.Add(fmt.Sprintf("<< /FunctionType 3 /Domain [0 1] /Functions [%d 0 R] /Encode [0 1] >>", deepest))
	})
	deep := syntax.Value{Kind: syntax.Ref, N: int64(deepest)}
	for i, c := range []struct {
		v  syntax.Value
		ok bool
	}{{deep, true}, {v, false}, {deep, true}, {v, false}} {
		if _, err := r.Check(c.v); (err == nil) != c.ok || err != nil && err != errDeep {
			t.Errorf("%d: %v", i, err)
		}
	}
}

// The work of reading is bounded over all a Reader reads; spent, a
// function is not read, and that is said.
func TestReaderWork(t *testing.T) {
	r, v := open(t, func(b *pdftest.Builder) int { return fan(b, 4, 2) })
	r.work = 5
	if _, err := r.Check(v); err != errSpent {
		t.Errorf("spent: %v", err)
	}
	r.work = maxWork
	must(t)(r.Check(v))
}

func FuzzCalculator(f *testing.F) {
	f.Add([]byte("{ 2 copy gt { exch } if pop 3 1 roll 1 index }"), 0.5, -2.0)
	f.Add([]byte("{ dup 0 lt { neg } { 1 2 bitshift } ifelse }"), 0.0, 1.0)
	f.Fuzz(func(t *testing.T, src []byte, a, b float64) {
		prog, err := compile(src)
		if err != nil {
			return
		}
		fn := &Func{typ: 4, m: 2, n: 2, domain: []float64{-1e6, 1e6, -1e6, 1e6}, prog: prog, stack: make([]value, maxStack)}
		out := make([]float64, 2)
		fn.Eval([]float64{a, b}, out)
		for _, x := range out {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				t.Fatalf("%v", out)
			}
		}
	})
}
