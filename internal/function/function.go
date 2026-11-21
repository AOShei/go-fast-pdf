// Package function evaluates the functions of a PDF file (ISO 32000-2,
// 7.10): sampled (type 0), exponential (type 2), stitching (type 3) and
// PostScript calculator (type 4) functions. A colour space made by a
// function, Separation or DeviceN, makes its colours with one.
//
// A function takes m numbers and gives n. Its inputs are clipped to its
// Domain, and its outputs to its Range where it has one. What a function
// cannot compute, a PostScript program that leaves too few numbers or
// divides by zero, gives zeros, clipped to its Range.
package function

import (
	"errors"
	"fmt"
	"math"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

const (
	// maxDepth bounds the functions within a function: those a stitching
	// function stitches, those of an array.
	maxDepth = 8
	// maxInputs and maxOutputs bound m and n. DeviceN has up to 32
	// colorants; a sampled function of more than maxSampledInputs inputs
	// is not interpolated, its corners being 2^m.
	maxInputs        = 32
	maxOutputs       = 32
	maxSampledInputs = 12
	// maxSamples bounds the sample values of a sampled function.
	maxSamples = 1 << 22
)

// Func is a function, parsed. It is not safe for concurrent use.
type Func struct {
	typ    int
	m, n   int
	domain []float64 // 2m
	rng    []float64 // 2n, nil for none
	cost   int

	// Type 0.
	size    []int
	encode  []float64 // 2m
	decode  []float64 // 2n
	samples []float32 // as decoded, n for each point, the first input fastest
	stride  []int     // of each input, in points

	// Type 2.
	c0, c1 []float64
	exp    float64

	// Type 3, and an array of functions of one output each (parts).
	funcs  []*Func
	bounds []float64
	enc3   []float64
	parts  bool

	// Type 4.
	prog  []instr
	stack []value

	in []float64 // scratch for the inputs of a stitched function
}

// In and Out are the numbers of the function's inputs and outputs.
func (fn *Func) In() int  { return fn.m }
func (fn *Func) Out() int { return fn.n }

// Cost is the work of one evaluation, about the operations it takes.
func (fn *Func) Cost() int { return fn.cost }

// Parse reads the function v: a function dictionary or stream, or an
// array of functions of one output each that together make one of as
// many outputs as there are functions. The samples of a sampled function
// are decoded up to limit bytes; with a limit of 0 they are not read, and
// the function is only checked. decoded says how many bytes were.
func Parse(f *file.File, v syntax.Value, limit int64) (fn *Func, decoded int64, err error) {
	p := parser{f: f, limit: limit, check: limit == 0}
	fn, err = p.parse(v, 0)
	return fn, p.decoded, err
}

type parser struct {
	f       *file.File
	limit   int64
	decoded int64
	check   bool
}

func (p *parser) parse(v syntax.Value, depth int) (*Func, error) {
	if depth > maxDepth {
		return nil, errors.New("functions nested too deeply")
	}
	v = p.f.Resolve(v)
	if v.Kind == syntax.Array {
		return p.array(v, depth)
	}
	if !v.IsDict() {
		return nil, errors.New("a function that is not a dictionary")
	}
	typ, _ := p.f.Resolve(v.Get("FunctionType")).Integer()
	fn := &Func{typ: int(typ)}
	var ok bool
	if fn.domain, ok = p.numbers(v.Get("Domain"), 2, maxInputs); !ok || !ordered(fn.domain) {
		return nil, fmt.Errorf("a function of type %d without a Domain", typ)
	}
	fn.m = len(fn.domain) / 2
	if r := v.Get("Range"); !r.IsNull() {
		if fn.rng, ok = p.numbers(r, 2, maxOutputs); !ok || !ordered(fn.rng) {
			return nil, fmt.Errorf("a function of type %d with a Range that is not one", typ)
		}
		fn.n = len(fn.rng) / 2
	}
	var err error
	switch typ {
	case 0:
		err = p.sampled(fn, v)
	case 2:
		err = p.exponential(fn, v)
	case 3:
		err = p.stitching(fn, v, depth)
	case 4:
		err = p.calculator(fn, v)
	default:
		return nil, fmt.Errorf("a function of type %d", typ)
	}
	if err != nil {
		return nil, err
	}
	if fn.n == 0 || fn.n > maxOutputs {
		return nil, fmt.Errorf("a function of type %d of %d outputs", typ, fn.n)
	}
	return fn, nil
}

// array makes one function of functions of one input and one output
// each.
func (p *parser) array(v syntax.Value, depth int) (*Func, error) {
	fn := &Func{parts: true, m: 1}
	if len(v.List) == 0 || len(v.List) > maxOutputs {
		return nil, fmt.Errorf("an array of %d functions", len(v.List))
	}
	for _, e := range v.List {
		g, err := p.parse(e, depth+1)
		if err != nil {
			return nil, err
		}
		if g.m != 1 || g.n != 1 {
			return nil, errors.New("an array of functions not each of one input and one output")
		}
		fn.funcs = append(fn.funcs, g)
		fn.cost += g.cost
	}
	fn.n = len(fn.funcs)
	return fn, nil
}

// numbers reads an array of even length from 2 to 2*max, of finite
// numbers.
func (p *parser) numbers(v syntax.Value, unit, max int) ([]float64, bool) {
	list := p.f.Resolve(v).Elems()
	if len(list) == 0 || len(list)%unit != 0 || len(list) > unit*max {
		return nil, false
	}
	out := make([]float64, len(list))
	for i, e := range list {
		x, ok := p.f.Resolve(e).Float()
		if !ok || math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, false
		}
		out[i] = x
	}
	return out, true
}

// ordered reports whether each pair of a list is a range.
func ordered(r []float64) bool {
	for i := 0; i+1 < len(r); i += 2 {
		if r[i] > r[i+1] {
			return false
		}
	}
	return true
}

// sampled reads a function of type 0: a table of samples over a grid of
// its inputs, Size points along each, which it interpolates.
func (p *parser) sampled(fn *Func, v syntax.Value) error {
	if fn.rng == nil {
		return errors.New("a sampled function without a Range")
	}
	if fn.m > maxSampledInputs {
		return fmt.Errorf("a sampled function of %d inputs", fn.m)
	}
	if v.Kind != syntax.Stream {
		return errors.New("a sampled function that is not a stream")
	}
	bps, _ := p.f.Resolve(v.Get("BitsPerSample")).Integer()
	switch bps {
	case 1, 2, 4, 8, 12, 16, 24, 32:
	default:
		return fmt.Errorf("a sampled function of %d bits a sample", bps)
	}
	sizes := p.f.Resolve(v.Get("Size")).Elems()
	if len(sizes) != fn.m {
		return errors.New("a sampled function whose Size is not of its inputs")
	}
	points := int64(1)
	fn.size = make([]int, fn.m)
	fn.stride = make([]int, fn.m)
	for i, s := range sizes {
		k, ok := p.f.Resolve(s).Integer()
		if !ok || k < 1 || k > maxSamples {
			return errors.New("a sampled function with a Size that is not one")
		}
		fn.size[i], fn.stride[i] = int(k), int(points)
		points *= k
		if points*int64(fn.n) > maxSamples {
			return fmt.Errorf("a sampled function of more than %d samples", maxSamples)
		}
	}
	fn.encode = make([]float64, 2*fn.m)
	for i := range fn.m {
		fn.encode[2*i+1] = float64(fn.size[i] - 1)
	}
	if e := v.Get("Encode"); !e.IsNull() {
		enc, ok := p.numbers(e, 2, maxInputs)
		if !ok || len(enc) != 2*fn.m {
			return errors.New("a sampled function with an Encode that is not of its inputs")
		}
		fn.encode = enc
	}
	fn.decode = fn.rng
	if d := v.Get("Decode"); !d.IsNull() {
		dec, ok := p.numbers(d, 2, maxOutputs)
		if !ok || len(dec) != 2*fn.n {
			return errors.New("a sampled function with a Decode that is not of its outputs")
		}
		fn.decode = dec
	}
	corners := 1 << fn.m
	fn.cost = corners*fn.n + 4*fn.m
	if p.check {
		return nil
	}
	need := (points*int64(fn.n)*bps + 7) / 8
	if need > p.limit-p.decoded {
		return file.ErrTruncated
	}
	data, err := p.f.Decode(v, nil, need)
	p.decoded += int64(len(data))
	if err != nil && !errors.Is(err, file.ErrTruncated) {
		return err
	}
	if int64(len(data)) < need {
		return fmt.Errorf("a sampled function of %d bytes of samples, its Size needing %d", len(data), need)
	}
	count := int(points) * fn.n
	fn.samples = make([]float32, count)
	maxv := math.Exp2(float64(bps)) - 1
	bit := 0
	for i := range fn.samples {
		var x uint64
		for read := 0; read < int(bps); {
			b := data[bit/8]
			take := min(8-bit%8, int(bps)-read)
			x = x<<take | uint64(b>>(8-bit%8-take))&(1<<take-1)
			read += take
			bit += take
		}
		j := i % fn.n
		lo, hi := fn.decode[2*j], fn.decode[2*j+1]
		fn.samples[i] = float32(lo + float64(x)*(hi-lo)/maxv)
	}
	return nil
}

// exponential reads a function of type 2, of one input: C0 + x^N (C1 -
// C0).
func (p *parser) exponential(fn *Func, v syntax.Value) error {
	if fn.m != 1 {
		return errors.New("an exponential function of more than one input")
	}
	n, ok := p.f.Resolve(v.Get("N")).Float()
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return errors.New("an exponential function without N")
	}
	fn.exp = n
	fn.c0, fn.c1 = []float64{0}, []float64{1}
	if c := v.Get("C0"); !c.IsNull() {
		if fn.c0, ok = p.numbers(c, 1, maxOutputs); !ok {
			return errors.New("an exponential function with a C0 that is not one")
		}
	}
	if c := v.Get("C1"); !c.IsNull() {
		if fn.c1, ok = p.numbers(c, 1, maxOutputs); !ok {
			return errors.New("an exponential function with a C1 that is not one")
		}
	}
	if len(fn.c0) != len(fn.c1) || fn.rng != nil && len(fn.c0) != fn.n {
		return errors.New("an exponential function whose C0, C1 and Range differ in length")
	}
	fn.n = len(fn.c0)
	fn.cost = 4 + fn.n
	return nil
}

// stitching reads a function of type 3, of one input: functions of
// subdomains of its Domain, split at Bounds, each mapped onto its own
// domain by Encode.
func (p *parser) stitching(fn *Func, v syntax.Value, depth int) error {
	if fn.m != 1 {
		return errors.New("a stitching function of more than one input")
	}
	list := p.f.Resolve(v.Get("Functions")).Elems()
	k := len(list)
	if k == 0 || k > 1<<10 {
		return fmt.Errorf("a stitching function of %d functions", k)
	}
	for _, e := range list {
		g, err := p.parse(e, depth+1)
		if err != nil {
			return err
		}
		if g.m != 1 || len(fn.funcs) > 0 && g.n != fn.funcs[0].n {
			return errors.New("a stitching function of functions of other inputs or outputs")
		}
		fn.funcs = append(fn.funcs, g)
		fn.cost = max(fn.cost, g.cost)
	}
	fn.cost += 4 + k
	if fn.rng != nil && fn.funcs[0].n != fn.n {
		return errors.New("a stitching function whose Range is not of its outputs")
	}
	fn.n = fn.funcs[0].n
	if k > 1 {
		b, ok := p.numbers(v.Get("Bounds"), 1, 1<<10)
		if !ok || len(b) != k-1 {
			return errors.New("a stitching function whose Bounds are not of its functions")
		}
		for i, x := range b {
			if i > 0 && x < b[i-1] || x < fn.domain[0] || x > fn.domain[1] {
				return errors.New("a stitching function whose Bounds are not in order")
			}
		}
		fn.bounds = b
	}
	enc, ok := p.numbers(v.Get("Encode"), 2, 1<<10)
	if !ok || len(enc) != 2*k {
		return errors.New("a stitching function whose Encode is not of its functions")
	}
	fn.enc3 = enc
	fn.in = make([]float64, 1)
	return nil
}

// Eval computes the function of in, len(in) of In(), into out, of
// Out().
func (fn *Func) Eval(in, out []float64) {
	switch {
	case fn.parts:
		for i, g := range fn.funcs {
			g.Eval(in[:1], out[i:i+1])
		}
		return
	case fn.typ == 0:
		fn.evalSampled(in, out)
	case fn.typ == 2:
		x := clip(in[0], fn.domain[0], fn.domain[1])
		t := math.Pow(x, fn.exp)
		for j := range out {
			out[j] = fn.c0[j] + t*(fn.c1[j]-fn.c0[j])
		}
	case fn.typ == 3:
		fn.evalStitching(in, out)
	case fn.typ == 4:
		fn.evalCalculator(in, out)
	}
	for j := range out {
		if math.IsNaN(out[j]) || math.IsInf(out[j], 0) {
			out[j] = 0
		}
		if fn.rng != nil {
			out[j] = clip(out[j], fn.rng[2*j], fn.rng[2*j+1])
		}
	}
}

func clip(x, lo, hi float64) float64 {
	if x < lo || math.IsNaN(x) {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// interpolate maps x from [a, b] onto [c, d].
func interpolate(x, a, b, c, d float64) float64 {
	if b == a {
		return c
	}
	return c + (x-a)*(d-c)/(b-a)
}

// evalSampled interpolates the samples about the point of in, linearly
// along each input. A cubic Order is interpolated linearly too.
func (fn *Func) evalSampled(in, out []float64) {
	if fn.samples == nil {
		// Only checked: its samples were not read.
		clear(out)
		return
	}
	var base int
	var frac [maxSampledInputs]float64
	var step [maxSampledInputs]int
	for i := range fn.m {
		x := clip(in[i], fn.domain[2*i], fn.domain[2*i+1])
		e := interpolate(x, fn.domain[2*i], fn.domain[2*i+1], fn.encode[2*i], fn.encode[2*i+1])
		e = clip(e, 0, float64(fn.size[i]-1))
		lo := int(e)
		if lo == fn.size[i]-1 {
			frac[i], step[i] = 0, 0
		} else {
			frac[i], step[i] = e-float64(lo), fn.stride[i]
		}
		base += lo * fn.stride[i]
	}
	for j := range out {
		out[j] = 0
	}
	for c := 0; c < 1<<fn.m; c++ {
		w, at := 1.0, base
		for i := range fn.m {
			if c>>i&1 == 1 {
				if step[i] == 0 {
					w = 0
					break
				}
				w *= frac[i]
				at += step[i]
			} else {
				w *= 1 - frac[i]
			}
		}
		if w == 0 {
			continue
		}
		s := fn.samples[at*fn.n:]
		for j := range out {
			out[j] += w * float64(s[j])
		}
	}
}

// evalStitching evaluates the function of the subdomain in[0] falls in.
func (fn *Func) evalStitching(in, out []float64) {
	x := clip(in[0], fn.domain[0], fn.domain[1])
	// Each subdomain holds its lower bound, the last its upper too. A
	// first one of no width, at the bottom of the Domain, holds that.
	i := 0
	if x > fn.domain[0] || len(fn.bounds) == 0 || fn.bounds[0] > x {
		for i < len(fn.bounds) && x >= fn.bounds[i] {
			i++
		}
	}
	lo, hi := fn.domain[0], fn.domain[1]
	if i > 0 {
		lo = fn.bounds[i-1]
	}
	if i < len(fn.bounds) {
		hi = fn.bounds[i]
	}
	fn.in[0] = interpolate(x, lo, hi, fn.enc3[2*i], fn.enc3[2*i+1])
	fn.funcs[i].Eval(fn.in, out)
}
