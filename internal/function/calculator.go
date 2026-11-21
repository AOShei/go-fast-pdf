package function

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// A PostScript calculator function (type 4) is a program of PostScript's
// operators on numbers and booleans, in braces: its inputs are put on a
// stack, and the program leaves its outputs there. It has no loops, only
// if and ifelse, so its work is bounded by its length. Integers and reals
// are both numbers here, as a renderer keeps them: idiv, mod and the
// bitwise operators truncate theirs to integers.

const (
	// maxStack is the depth of the operand stack a program may use, and
	// maxProgram the length of a program, in operators and numbers.
	maxStack   = 100
	maxProgram = 1 << 16
	// maxBraces bounds the nesting of a program's braces.
	maxBraces = 100
)

type opcode uint8

const (
	opPush opcode = iota
	opJumpFalse
	opJump
	opAbs
	opAdd
	opAnd
	opAtan
	opBitshift
	opCeiling
	opCopy
	opCos
	opCvi
	opCvr
	opDiv
	opDup
	opEq
	opExch
	opExp
	opFalse
	opFloor
	opGe
	opGt
	opIdiv
	opIndex
	opLe
	opLn
	opLog
	opLt
	opMod
	opMul
	opNe
	opNeg
	opNot
	opOr
	opPop
	opRoll
	opRound
	opSin
	opSqrt
	opSub
	opTrue
	opTruncate
	opXor
)

var operators = map[string]opcode{
	"abs": opAbs, "add": opAdd, "and": opAnd, "atan": opAtan, "bitshift": opBitshift,
	"ceiling": opCeiling, "copy": opCopy, "cos": opCos, "cvi": opCvi, "cvr": opCvr,
	"div": opDiv, "dup": opDup, "eq": opEq, "exch": opExch, "exp": opExp, "false": opFalse,
	"floor": opFloor, "ge": opGe, "gt": opGt, "idiv": opIdiv, "index": opIndex, "le": opLe,
	"ln": opLn, "log": opLog, "lt": opLt, "mod": opMod, "mul": opMul, "ne": opNe,
	"neg": opNeg, "not": opNot, "or": opOr, "pop": opPop, "roll": opRoll, "round": opRound,
	"sin": opSin, "sqrt": opSqrt, "sub": opSub, "true": opTrue, "truncate": opTruncate,
	"xor": opXor,
}

// instr is one step of a program: an operator, a number to push, or a
// jump to target.
type instr struct {
	op     opcode
	target int32
	x      float64
}

// value is a number, or a boolean (1 for true).
type value struct {
	x       float64
	boolean bool
}

// calculator reads a function of type 4.
func (p *parser) calculator(fn *Func, v syntax.Value) error {
	if fn.rng == nil {
		return errors.New("a PostScript calculator function without a Range")
	}
	if v.Kind != syntax.Stream {
		return errors.New("a PostScript calculator function that is not a stream")
	}
	// A program is short; one longer than maxProgram tokens could be is
	// not read.
	data, err := p.f.Decode(v, nil, 16*maxProgram)
	if err != nil {
		return fmt.Errorf("a PostScript calculator function: %w", err)
	}
	prog, err := compile(data)
	if err != nil {
		return err
	}
	fn.prog = prog
	fn.cost = len(prog) + fn.m + fn.n
	fn.stack = make([]value, maxStack)
	return nil
}

// compile makes the instructions of a program.
func compile(src []byte) ([]instr, error) {
	c := compiler{src: src}
	c.space()
	if c.pos >= len(c.src) || c.src[c.pos] != '{' {
		return nil, errors.New("a PostScript calculator function that does not start with a brace")
	}
	c.pos++
	prog, err := c.block(0)
	if err != nil {
		return nil, err
	}
	c.space()
	if c.pos < len(c.src) {
		return nil, errors.New("a PostScript calculator function with more after its program")
	}
	return prog, nil
}

type compiler struct {
	src    []byte
	pos    int
	length int
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t' || b == '\f' || b == 0
}

// space skips white space and comments.
func (c *compiler) space() {
	for c.pos < len(c.src) {
		switch b := c.src[c.pos]; {
		case isSpace(b):
			c.pos++
		case b == '%':
			for c.pos < len(c.src) && c.src[c.pos] != '\n' && c.src[c.pos] != '\r' {
				c.pos++
			}
		default:
			return
		}
	}
}

// block compiles up to the brace that closes the one before it. The
// blocks of if and ifelse are written before them, so they are kept
// until their operator comes.
func (c *compiler) block(depth int) ([]instr, error) {
	if depth > maxBraces {
		return nil, errors.New("a PostScript calculator function nested too deeply")
	}
	var out []instr
	var procs [][]instr
	for {
		c.space()
		if c.pos >= len(c.src) {
			return nil, errors.New("a PostScript calculator function without its closing brace")
		}
		switch c.src[c.pos] {
		case '{':
			c.pos++
			proc, err := c.block(depth + 1)
			if err != nil {
				return nil, err
			}
			if len(procs) == 2 {
				return nil, errors.New("a PostScript calculator function with a procedure that is not run")
			}
			procs = append(procs, proc)
			continue
		case '}':
			c.pos++
			if len(procs) > 0 {
				return nil, errors.New("a PostScript calculator function with a procedure that is not run")
			}
			return out, nil
		}
		start := c.pos
		for c.pos < len(c.src) && !isSpace(c.src[c.pos]) && c.src[c.pos] != '{' && c.src[c.pos] != '}' && c.src[c.pos] != '%' {
			c.pos++
		}
		tok := string(c.src[start:c.pos])
		if c.length++; c.length > maxProgram {
			return nil, errors.New("a PostScript calculator function too long")
		}
		switch {
		case tok == "if" && len(procs) == 1:
			out = append(out, instr{op: opJumpFalse, target: int32(len(out) + 1 + len(procs[0]))})
			out = append(out, relocate(procs[0], len(out))...)
			procs = procs[:0]
			continue
		case tok == "ifelse" && len(procs) == 2:
			// jump-if-false to the second; the first; jump past the second.
			first, second := procs[0], procs[1]
			elseAt := len(out) + 1 + len(first) + 1
			out = append(out, instr{op: opJumpFalse, target: int32(elseAt)})
			out = append(out, relocate(first, len(out))...)
			out = append(out, instr{op: opJump, target: int32(elseAt + len(second))})
			out = append(out, relocate(second, len(out))...)
			procs = procs[:0]
			continue
		case len(procs) > 0:
			return nil, fmt.Errorf("a PostScript calculator function with a procedure before %q", printable(tok))
		}
		if op, ok := operators[tok]; ok {
			out = append(out, instr{op: op})
			continue
		}
		v, ok := number(tok)
		if !ok {
			return nil, fmt.Errorf("a PostScript calculator function with the operator %q", printable(tok))
		}
		out = append(out, instr{op: opPush, x: v})
	}
}

// number reads a number of PostScript's: an integer or a real, with an
// exponent or without. Radix numbers (16#FF) are not read.
func number(tok string) (float64, bool) {
	if strings.Trim(tok, "0123456789+-.eE") != "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(tok, 64)
	return v, err == nil
}

// relocate moves the jumps of a block compiled on its own to where it
// is put.
func relocate(proc []instr, at int) []instr {
	for i := range proc {
		if proc[i].op == opJump || proc[i].op == opJumpFalse {
			proc[i].target += int32(at)
		}
	}
	return proc
}

// printable cuts a token for a message, and leaves only its printable
// ASCII.
func printable(s string) string {
	b := []byte(s[:min(len(s), 32)])
	for i, c := range b {
		if c < 0x20 || c > 0x7E {
			b[i] = '?'
		}
	}
	return string(b)
}

// evalCalculator runs the program on the inputs. A program that fails,
// or leaves fewer numbers than the outputs, gives zeros.
func (fn *Func) evalCalculator(in, out []float64) {
	st := fn.stack
	for i := range fn.m {
		st[i] = value{x: clip(in[i], fn.domain[2*i], fn.domain[2*i+1])}
	}
	sp, ok := run(fn.prog, st, fn.m)
	if !ok || sp < fn.n {
		for j := range out {
			out[j] = 0
		}
		return
	}
	for j := range out {
		out[j] = st[sp-fn.n+j].x
	}
}

// toInt is a number as a PostScript integer, truncated.
func toInt(x float64) int64 {
	if math.IsNaN(x) {
		return 0
	}
	return int64(int32(max(min(x, math.MaxInt32), math.MinInt32)))
}

func boolOf(b bool) value {
	if b {
		return value{x: 1, boolean: true}
	}
	return value{boolean: true}
}

// run runs a program on a stack holding sp values, and returns the
// number it holds after; ok is false when it failed.
func run(prog []instr, st []value, sp int) (int, bool) {
	for pc := 0; pc < len(prog); pc++ {
		in := &prog[pc]
		// Each operator's operands, and room for its results.
		var need, room int
		switch in.op {
		case opPush, opTrue, opFalse:
			room = 1
		case opDup:
			need, room = 1, 1
		case opJumpFalse, opJump, opPop, opAbs, opCeiling, opCos, opCvi, opCvr, opFloor, opLn, opLog,
			opNeg, opNot, opRound, opSin, opSqrt, opTruncate, opIndex, opCopy:
			need = 1
		case opRoll:
			need = 2
		default:
			need = 2
		}
		if in.op == opJump {
			need = 0
		}
		if sp < need || sp+room > len(st) {
			return sp, false
		}
		switch in.op {
		case opPush:
			st[sp] = value{x: in.x}
			sp++
		case opTrue, opFalse:
			st[sp] = boolOf(in.op == opTrue)
			sp++
		case opJumpFalse:
			sp--
			if st[sp].x == 0 {
				pc = int(in.target) - 1
			}
		case opJump:
			pc = int(in.target) - 1
		case opDup:
			st[sp] = st[sp-1]
			sp++
		case opPop:
			sp--
		case opExch:
			st[sp-1], st[sp-2] = st[sp-2], st[sp-1]
		case opIndex:
			n := toInt(st[sp-1].x)
			if n < 0 || n > int64(sp-2) {
				return sp, false
			}
			st[sp-1] = st[sp-2-int(n)]
		case opCopy:
			n := toInt(st[sp-1].x)
			sp--
			if n < 0 || n > int64(sp) || sp+int(n) > len(st) {
				return sp, false
			}
			copy(st[sp:], st[sp-int(n):sp])
			sp += int(n)
		case opRoll:
			n, j := toInt(st[sp-2].x), toInt(st[sp-1].x)
			sp -= 2
			if n < 0 || n > int64(sp) {
				return sp, false
			}
			if n > 0 {
				s := st[sp-int(n) : sp]
				k := int(((j % n) + n) % n)
				// Rotate s up by k: reverse all, then each part.
				reverse(s)
				reverse(s[:k])
				reverse(s[k:])
			}
		case opAbs:
			st[sp-1].x = math.Abs(st[sp-1].x)
		case opNeg:
			st[sp-1].x = -st[sp-1].x
		case opCeiling:
			st[sp-1].x = math.Ceil(st[sp-1].x)
		case opFloor:
			st[sp-1].x = math.Floor(st[sp-1].x)
		case opRound:
			st[sp-1].x = math.Floor(st[sp-1].x + 0.5)
		case opTruncate:
			st[sp-1].x = math.Trunc(st[sp-1].x)
		case opCvi:
			st[sp-1].x = float64(toInt(st[sp-1].x))
		case opCvr:
		case opSqrt:
			if st[sp-1].x < 0 {
				return sp, false
			}
			st[sp-1].x = math.Sqrt(st[sp-1].x)
		case opSin:
			st[sp-1].x = math.Sin(st[sp-1].x * math.Pi / 180)
		case opCos:
			st[sp-1].x = math.Cos(st[sp-1].x * math.Pi / 180)
		case opLn, opLog:
			if st[sp-1].x <= 0 {
				return sp, false
			}
			if in.op == opLn {
				st[sp-1].x = math.Log(st[sp-1].x)
			} else {
				st[sp-1].x = math.Log10(st[sp-1].x)
			}
		case opNot:
			if st[sp-1].boolean {
				st[sp-1] = boolOf(st[sp-1].x == 0)
			} else {
				st[sp-1].x = float64(^toInt(st[sp-1].x))
			}
		default:
			a, b := st[sp-2], st[sp-1]
			sp--
			r := value{}
			switch in.op {
			case opAdd:
				r.x = a.x + b.x
			case opSub:
				r.x = a.x - b.x
			case opMul:
				r.x = a.x * b.x
			case opDiv:
				if b.x == 0 {
					return sp, false
				}
				r.x = a.x / b.x
			case opIdiv, opMod:
				x, y := toInt(a.x), toInt(b.x)
				if y == 0 {
					return sp, false
				}
				if in.op == opIdiv {
					r.x = float64(x / y)
				} else {
					r.x = float64(x % y)
				}
			case opExp:
				r.x = math.Pow(a.x, b.x)
			case opAtan:
				if a.x == 0 && b.x == 0 {
					return sp, false
				}
				d := math.Atan2(a.x, b.x) * 180 / math.Pi
				if d < 0 {
					d += 360
				}
				r.x = d
			case opBitshift:
				x, s := toInt(a.x), toInt(b.x)
				switch {
				case s >= 32 || s <= -32:
					r.x = 0
				case s >= 0:
					r.x = float64(int32(x << s))
				default:
					r.x = float64(int32(x) >> -s)
				}
			case opAnd, opOr, opXor:
				if a.boolean && b.boolean {
					p, q := a.x != 0, b.x != 0
					switch in.op {
					case opAnd:
						r = boolOf(p && q)
					case opOr:
						r = boolOf(p || q)
					default:
						r = boolOf(p != q)
					}
				} else {
					x, y := toInt(a.x), toInt(b.x)
					switch in.op {
					case opAnd:
						r.x = float64(x & y)
					case opOr:
						r.x = float64(x | y)
					default:
						r.x = float64(x ^ y)
					}
				}
			case opEq:
				r = boolOf(a.x == b.x)
			case opNe:
				r = boolOf(a.x != b.x)
			case opGe:
				r = boolOf(a.x >= b.x)
			case opGt:
				r = boolOf(a.x > b.x)
			case opLe:
				r = boolOf(a.x <= b.x)
			case opLt:
				r = boolOf(a.x < b.x)
			}
			st[sp-1] = r
		}
	}
	return sp, true
}

func reverse(s []value) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
