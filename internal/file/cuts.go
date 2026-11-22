package file

import (
	"fmt"
	"sort"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Cuts counts what the fixed bounds of the library left out of what
// reaches the output, by bound. Each bound gives one limit.exceeded, an
// error, made when the counting is over, with what was counted: so the
// warning does not depend on the order in which pages or objects were
// read. The zero Cuts is empty.
type Cuts struct {
	by map[string]*cutCount
}

// A Bound says what a fixed bound leaves out. Its message is made from
// the counts: n things, and where the bound cuts text, bytes left out.
type Bound struct {
	// Key names the bound; bounds are given in the order of their keys.
	Key     string
	Message func(n, bytes int64) string
}

type cutCount struct {
	bound    *Bound
	n, bytes int64
	seen     map[string]bool // the ids counted by AddOnce
}

// Add counts n things, and bytes, that the bound left out.
func (c *Cuts) Add(b *Bound, n, bytes int64) {
	if c.by == nil {
		c.by = map[string]*cutCount{}
	}
	cc := c.by[b.Key]
	if cc == nil {
		cc = &cutCount{bound: b}
		c.by[b.Key] = cc
	}
	cc.n += n
	cc.bytes += bytes
}

// AddOnce counts n things, and bytes, that the bound left out of what id
// names, unless they were counted for id already: what more than one
// path of the reading reaches, such as the name of a form field, is
// counted once.
func (c *Cuts) AddOnce(b *Bound, id string, n, bytes int64) {
	if c.by == nil {
		c.by = map[string]*cutCount{}
	}
	cc := c.by[b.Key]
	if cc == nil {
		cc = &cutCount{bound: b}
		c.by[b.Key] = cc
	}
	if cc.seen[id] {
		return
	}
	if cc.seen == nil {
		cc.seen = map[string]bool{}
	}
	cc.seen[id] = true
	cc.n += n
	cc.bytes += bytes
}

// Warnings gives the warning of each bound that left something out.
func (c *Cuts) Warnings() []gofastcore.Warning {
	keys := make([]string, 0, len(c.by))
	for k := range c.by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]gofastcore.Warning, 0, len(keys))
	for _, k := range keys {
		cc := c.by[k]
		out = append(out, gofastcore.Warning{Code: gofastcore.WarnLimitExceeded, Severity: gofastcore.SeverityError,
			Message: cc.bound.Message(cc.n, cc.bytes)})
	}
	return out
}

// AddAll counts what o counted, o's ids included.
func (c *Cuts) AddAll(o *Cuts) {
	for _, oc := range o.by {
		if len(oc.seen) == 0 {
			c.Add(oc.bound, oc.n, oc.bytes)
			continue
		}
		c.Add(oc.bound, 0, 0)
		cc := c.by[oc.bound.Key]
		if cc.seen == nil {
			cc.seen = map[string]bool{}
		}
		for id := range oc.seen {
			cc.seen[id] = true
		}
		cc.n += oc.n
		cc.bytes += oc.bytes
	}
}

// Reset empties c, keeping its room.
func (c *Cuts) Reset() { clear(c.by) }

// The two shapes of the messages, the same in every go-fast library:
// what was cut at a length, and what was not read past a number.

// CutMessage is "X longer than N bytes were cut there (K units, B bytes
// left out)", for a bound on the length of a text.
func CutMessage(things, units string, limit int) func(n, bytes int64) string {
	return func(n, bytes int64) string {
		return fmt.Sprintf("%s longer than %d bytes were cut there (%d %s, %d bytes left out)", things, limit, n, units, bytes)
	}
}

// PastMessage is "X past N were not read (K units)", for a bound on a
// number of things that are counted.
func PastMessage(things, units string, limit int) func(n, bytes int64) string {
	return func(n, _ int64) string {
		return fmt.Sprintf("%s past %d were not read (%d %s)", things, limit, n, units)
	}
}

// MoreMessage is "the W has more than N X; the rest were not read", for
// a bound where reading stops and what follows is not counted. n counts
// the places where it stopped, and is given when there were several.
func MoreMessage(where, things string, limit int) func(n, bytes int64) string {
	return func(n, _ int64) string {
		m := fmt.Sprintf("%s has more than %d %s; the rest were not read", where, limit, things)
		if n > 1 {
			m += fmt.Sprintf(" (%d times)", n)
		}
		return m
	}
}

// Cut counts what a bound left out of the document as a whole.
func (f *File) Cut(b *Bound, n, bytes int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cuts.Add(b, n, bytes)
}

// CutOnce is Cut of what id names, counted once (Cuts.AddOnce).
func (f *File) CutOnce(b *Bound, id string, n, bytes int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cuts.AddOnce(b, id, n, bytes)
}
