package gofastpdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
)

// File is an open PDF file. Nothing of its pages is read until a page is
// asked for, and no page is kept once it has been handed out, so a file
// of any number of pages can be gone through in the memory that the
// file itself and a few pages take.
//
// A File is safe for use by several goroutines at once. Close it when
// done with it.
//
// The whole file is held in memory while it is open: a PDF is read from
// its end and by offsets all over it.
type File struct {
	e    *Extractor
	f    *file.File
	opts gofastcore.Options
	name string
	size int64

	pages []file.Page
	// The pages that Options.Parts and Limits.MaxParts select: the range
	// in pages, how many were asked for, and whether the limit cut them.
	lo, hi  int
	asked   int
	tooMany bool
	budget  int64 // Limits.MaxDecodedBytes, resolved
	// textLimit is Limits.MaxTextBytes, resolved. It bounds the outline
	// when it is read; a reading of the pages counts against a budget
	// made from it.
	textLimit int64

	once sync.Once
	desc described

	// What the pages read through this handle have shown so far.
	incomplete, active, external atomic.Bool
	closed                       atomic.Bool
}

// ErrClosed is returned by the methods of a File that has been closed.
var ErrClosed = errors.New("gofastpdf: the file is closed")

// Open opens the PDF file at path. opts may be nil; see the package
// documentation for what each option does. An error wraps one of the
// errors of gofastcore: ErrMalformed, ErrPasswordRequired,
// ErrWrongPassword, ErrUnsupported, ErrLimitExceeded,
// ErrNoPartsSelected.
func Open(path string, opts *gofastcore.Options) (*File, error) {
	return new(Extractor).Open(path, opts)
}

// OpenBytes opens a PDF file that is in memory. The data is used as it
// is, not copied: it must not change while the File is open.
func OpenBytes(data []byte, opts *gofastcore.Options) (*File, error) {
	return new(Extractor).OpenBytes(data, opts)
}

// OpenReaderAt opens a PDF file of size bytes read from r. All of it is
// read at once; r is not used after OpenReaderAt returns.
func OpenReaderAt(r io.ReaderAt, size int64, opts *gofastcore.Options) (*File, error) {
	return new(Extractor).OpenReaderAt(r, size, opts)
}

// Open opens the PDF file at path. See the function Open.
func (e *Extractor) Open(path string, opts *gofastcore.Options) (*File, error) {
	in, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return nil, err
	}
	f, err := e.OpenReaderAt(in, info.Size(), opts)
	if err != nil {
		return nil, err
	}
	if f.name == "" {
		f.name = filepath.Base(path)
	}
	return f, nil
}

// OpenReaderAt opens a PDF file of size bytes read from r. See the
// function OpenReaderAt.
func (e *Extractor) OpenReaderAt(r io.ReaderAt, size int64, opts *gofastcore.Options) (*File, error) {
	if err := checkSize(size, opts); err != nil {
		return nil, err
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(io.NewSectionReader(r, 0, size), data); err != nil {
		return nil, fmt.Errorf("reading the input: %w", err)
	}
	return e.OpenBytes(data, opts)
}

// checkSize refuses an input that is larger than the limit before any
// of it is read.
func checkSize(size int64, opts *gofastcore.Options) error {
	if size < 0 {
		return fmt.Errorf("%w: negative size", gofastcore.ErrMalformed)
	}
	var limit int64
	if opts != nil {
		limit = opts.Limits.MaxInputBytes
	}
	if limit = gofastcore.ResolveLimit(limit, DefaultMaxInputBytes); size > limit {
		return fmt.Errorf("%w: the input is %d bytes and the limit is %d", gofastcore.ErrLimitExceeded, size, limit)
	}
	return nil
}

// OpenBytes opens a PDF file that is in memory. See the function
// OpenBytes.
func (e *Extractor) OpenBytes(data []byte, opts *gofastcore.Options) (*File, error) {
	if opts == nil {
		opts = &gofastcore.Options{}
	}
	if err := checkSize(int64(len(data)), opts); err != nil {
		return nil, err
	}
	budget := gofastcore.ResolveLimit(opts.Limits.MaxDecodedBytes, DefaultMaxDecodedBytes)
	stream := e.MaxStreamBytes
	if stream <= 0 {
		stream = maxStreamBytes
	}
	limits := file.Limits{MaxStreamBytes: min(budget, stream), ObjStmCacheBytes: e.ObjectStreamCacheBytes, ObjCacheBytes: e.ObjectCacheBytes}
	if opts.Limits.MaxDecodedBytes < 0 && e.MaxStreamBytes <= 0 {
		limits.MaxStreamBytes = 1 << 40
	}
	inner, err := file.OpenKeys(data, limits, opts.Password, opts.Keys)
	if err != nil {
		return nil, err
	}
	pages := inner.Pages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: %w", gofastcore.ErrMalformed, file.ErrNoPages)
	}

	// The pages asked for.
	rng := opts.Parts
	if rng.First < 1 {
		rng.First = 1
	}
	if rng.Last < 0 || rng.Last != 0 && rng.First > rng.Last || rng.First > len(pages) {
		return nil, fmt.Errorf("%w: pages %d to %d of a document of %d", gofastcore.ErrNoPartsSelected, opts.Parts.First, opts.Parts.Last, len(pages))
	}
	if rng.Last == 0 || rng.Last > len(pages) {
		rng.Last = len(pages)
	}
	f := &File{e: e, f: inner, opts: *opts, name: opts.Name, size: int64(len(data)), pages: pages, budget: budget,
		textLimit: gofastcore.ResolveLimit(opts.Limits.MaxTextBytes, gofastcore.DefaultMaxTextBytes)}
	f.lo, f.hi, f.asked = rng.First-1, rng.Last, rng.Last-rng.First+1
	if most := gofastcore.ResolveLimit(int64(opts.Limits.MaxParts), math.MaxInt64); int64(f.asked) > most {
		f.hi, f.tooMany = f.lo+int(most), true
	}
	return f, nil
}

// Close releases the file. The parts, pages and lines that were handed
// out stay valid. Closing twice is not an error.
func (f *File) Close() error {
	f.closed.Store(true)
	return nil
}

// NumPages returns the number of pages in the file, whatever range of
// them the options select.
func (f *File) NumPages() int { return len(f.pages) }

// Metadata returns what the file says about itself: title, author,
// dates, the program that made it. No page is read for it.
func (f *File) Metadata() gofastcore.Metadata {
	md := f.describe().meta
	md.Authors = append([]string(nil), md.Authors...)
	md.Keywords = append([]string(nil), md.Keywords...)
	if md.Extra != nil {
		extra := make(map[string]string, len(md.Extra))
		for k, v := range md.Extra {
			extra[k] = v
		}
		md.Extra = extra
	}
	return md
}

// Outline returns the bookmarks of the file, each with the page it
// points at. No page is read for it.
func (f *File) Outline() []gofastcore.OutlineItem {
	return append([]gofastcore.OutlineItem(nil), f.describe().outline...)
}

// HiddenChecked returns the hidden reasons that were checked completely
// on the pages read so far, as Document.HiddenChecked says it of a
// whole document.
//
// The value is final only when every part has been produced. The six
// reasons of the default checks are always in it. With
// Options.DeepHidden, clipped is too; low_contrast and occluded are
// until a page is read that paints more shapes than are followed, and
// not from then on. A caller who stops early may rely on the six, and on
// the three others for every part received that does not have the
// pdf.deep_hidden_incomplete warning.
func (f *File) HiddenChecked() gofastcore.Hidden {
	return f.checked(f.incomplete.Load())
}

func (f *File) checked(incomplete bool) gofastcore.Hidden {
	h := hiddenChecked
	if f.opts.DeepHidden && !f.e.noHiddenChecks {
		h |= gofastcore.HiddenClipped
		if !incomplete {
			h |= gofastcore.HiddenLowContrast | gofastcore.HiddenOccluded
		}
	}
	return h
}

// Warnings returns what concerns the file as a whole, as the warnings
// of a Document do: that it was read through repair, that it holds
// scripts, that it points outside itself. What is known without reading
// a page is there from the start; a page that is read can add
// input.active_content or input.external_reference, so the value is
// final only when every part has been produced.
func (f *File) Warnings() []gofastcore.Warning {
	d := f.describe()
	ws := f.warnings(d.active || f.active.Load(), d.external || f.external.Load())
	if d.invisible {
		ws = append(ws, gofastcore.Warning{Code: gofastcore.WarnTextHidden, Severity: gofastcore.SeverityWarning,
			Message: "the document information or the outline has invisible characters"})
	}
	if d.outlineCut {
		ws = append(ws, gofastcore.Warning{Code: gofastcore.WarnLimitExceeded, Severity: gofastcore.SeverityError,
			Message: fmt.Sprintf("the titles of the outline are more than the limit on text of %d bytes; the outline ends after %d entries", f.textLimit, len(d.outline))})
	}
	return ws
}

// warnings returns the warnings of the file itself, in an order that
// does not depend on which worker found what.
func (f *File) warnings(active, external bool) []gofastcore.Warning {
	ws := f.f.Warnings()
	for i := range ws {
		ws[i].Message = cleanString(ws[i].Message)
	}
	add := func(severity gofastcore.Severity, code, message string) {
		ws = append(ws, gofastcore.Warning{Code: code, Severity: severity, Message: message})
	}
	if rebuilt, redefined := f.f.Rebuilt(); rebuilt && redefined > 0 {
		add(gofastcore.SeverityWarning, gofastcore.WarnInputAmbiguous,
			fmt.Sprintf("the cross-reference data is unusable and %d objects are defined more than once; the last definition of each was read", redefined))
	}
	if active {
		add(gofastcore.SeverityInfo, gofastcore.WarnInputActiveContent, "the document has scripts or actions that start a program; none was read or run")
	}
	if external {
		add(gofastcore.SeverityWarning, gofastcore.WarnInputExternalReference, "the document refers to files or addresses outside itself other than by links; nothing was fetched")
	}
	return ws
}

// state is what putting parts together in page order carries from one
// part to the next: what is left of the limits on decoded bytes and on
// text, and which images have had their data.
type state struct {
	f      *File
	budget int64
	images map[string]bool
	// imagesCut and attachmentsCut are set when data was left out because
	// the budget ran out.
	imagesCut, attachmentsCut bool

	// decodeLimit is what budget started at: the limit of this file, or
	// what the document it is embedded in had left, if that is less.
	decodeLimit int64
	// dec is the count of decoded bytes that this document shares with
	// the documents embedded in it and with the one it is embedded in.
	// The pages are counted in budget; the two are made to agree before
	// an embedded file is handed on and when the reading ends.
	dec *gofastcore.DecodeBudget
	// text counts the text of the document against Limits.MaxTextBytes.
	text      *gofastcore.TextBudget
	textLimit int64
	// outline is the number of entries of the outline that the text
	// budget had room for, and outlineCut says that those are not all.
	outline    int
	outlineCut bool
	// unread is set when an attached file was not read because the text
	// budget was spent.
	unread bool
}

// newState is the state for a page read by itself: the limits apply to
// it alone.
func (f *File) newState() *state {
	_, text := gofastcore.WithTextBudget(context.Background(), f.opts.Limits)
	return &state{f: f, budget: f.budget, decodeLimit: f.budget, images: map[string]bool{}, text: text, textLimit: text.Left()}
}

// begin is the state for one reading of the pages in order, and the
// context that carries its budgets to the extractors of attached files.
// The budgets are this file's, or what the document it is embedded in
// has left of its own, which ctx says.
//
// The titles of the outline are taken from the text budget here, before
// any page: where a document is cut must not depend on when its outline
// happens to be read.
func (f *File) begin(ctx context.Context) (context.Context, *state) {
	st := &state{f: f, images: map[string]bool{}}
	ctx, st.dec = gofastcore.WithDecodeBudget(ctx, f.opts.Limits, DefaultMaxDecodedBytes)
	st.budget = st.dec.Left()
	st.decodeLimit = st.budget
	ctx, st.text = gofastcore.WithTextBudget(ctx, f.opts.Limits)
	st.textLimit = st.text.Left()
	d := f.describe()
	for st.outline < len(d.outline) {
		if !st.text.Take(len(d.outline[st.outline].Title)) {
			st.outlineCut = true
			break
		}
		st.outline++
	}
	if d.outlineCut && !st.outlineCut {
		// The outline was cut when it was read, at the limit of this
		// file. Nothing is left for the pages.
		st.outlineCut = true
		for !st.text.Spent() {
			st.text.Take(math.MaxInt32)
		}
	}
	return ctx, st
}

// end closes the budgets: what this document used is taken from the
// document it is embedded in.
func (st *state) end() {
	st.settle()
	st.dec.Close()
	st.text.Close()
}

// settle makes the shared count of decoded bytes say what this document
// has left.
func (st *state) settle() {
	st.dec.Take(st.dec.Left() - st.budget)
}

// stored is the part for what the file holds and shows on no page:
// scripts, entries of its own in the document information, what its
// metadata packet says beyond that, fields of a form that are on no
// page. It comes after the pages and is numbered after them, and it is
// there only when the whole document was asked for and read to its end.
// ok is false when there is no such part, and whole when the limit on
// text cut it.
func (st *state) stored() (part gofastcore.Part, ok, whole bool) {
	f := st.f
	d := f.describe()
	if len(d.stored) == 0 || f.opts.Parts != (gofastcore.PartRange{}) || f.tooMany {
		return part, false, true
	}
	part = gofastcore.Part{Kind: gofastcore.PartStored, Number: len(f.pages) + 1, Hidden: gofastcore.HiddenInvisible}
	// A copy: the part is the caller's to change, and what was read is
	// read once for the file.
	part.Blocks = make([]gofastcore.Block, len(d.stored))
	for i, b := range d.stored {
		attrs := make(map[string]string, len(b.Attrs))
		for k, v := range b.Attrs {
			attrs[k] = v
		}
		b.Attrs = attrs
		part.Blocks[i] = b
	}
	if d.storedCut {
		part.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the file stores more than %d bytes of text that it shows on no page; the rest is not read", maxStoredBytes))
	}
	whole = st.charge(&part)
	part.MarkHidden()
	return part, true, whole
}

// charge takes a part from the budget for text: its name, where the
// file gives it one, and for each of its blocks the text and gofastcore.BlockCost, which is
// the rule of the shared model: a block of no text is not free.
// It reports whether all of it was there to take. If not, the part is
// cut before the block that did not fit, and says so; nothing after it
// is read.
func (st *state) charge(part *gofastcore.Part) bool {
	ok := st.text.Take(len(part.Name))
	if !ok {
		part.Name = ""
	}
	kept := 0
	for ok && kept < len(part.Blocks) {
		if ok = st.text.Take(gofastcore.BlockCost + len(part.Blocks[kept].Text)); ok {
			kept++
		}
	}
	if ok {
		return true
	}
	clear(part.Blocks[kept:])
	part.Blocks = part.Blocks[:kept]
	part.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
		fmt.Sprintf("the text and the blocks of the document are more than the limit on text of %d bytes; this part is cut where the limit was reached", st.textLimit))
	return false
}

// finish completes the part of one page: the name its label gives it,
// and the IDs and data of its images.
func (st *state) finish(out *pageOut) {
	part := &out.part
	if label := st.f.describe().label; label != nil {
		if name := label(part.Number - 1); name != "" && name != strconv.Itoa(part.Number) {
			part.Name = name
		}
	}
	inline := 0
	for _, ref := range out.images {
		st.image(part, ref, &inline)
	}
}

// note keeps what a page that was read says about the file.
func (f *File) note(out *pageOut) {
	if out.deepIncomplete {
		f.incomplete.Store(true)
	}
	if out.active {
		f.active.Store(true)
	}
	if out.external {
		f.external.Store(true)
	}
}

// each reads the selected pages and calls fn with each in page order,
// until fn returns false. Several pages are read at a time, and no more
// than a few are held that fn has not had yet. It returns the error of
// ctx if that ended it.
func (f *File) each(ctx context.Context, w want, fn func(out *pageOut) bool) error {
	pages := f.pages[f.lo:f.hi]
	workers := f.opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	workers = min(workers, len(pages))

	// The page being read is abandoned when the context ends.
	var stop atomic.Bool
	w.stop = &stop
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(ctx, func() { stop.Store(true) })()

	if workers <= 1 {
		for i := range pages {
			if err := ctx.Err(); err != nil {
				return err
			}
			out := f.e.page(f.f, &pages[i], &f.opts, w)
			if err := ctx.Err(); err != nil {
				return err
			}
			if !fn(&out) {
				return nil
			}
		}
		return nil
	}

	// A feeder hands the pages to the workers in order and queues, in the
	// same order, the place each result will arrive at. The queue is
	// bounded, so the workers cannot run far ahead of fn.
	type job struct {
		page *file.Page
		out  chan pageOut
	}
	jobs := make(chan job)
	queue := make(chan chan pageOut, 2*workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				j.out <- f.e.page(f.f, j.page, &f.opts, w)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(jobs)
		defer close(queue)
		for i := range pages {
			out := make(chan pageOut, 1)
			select {
			case queue <- out:
			case <-ctx.Done():
				return
			}
			select {
			case jobs <- job{&pages[i], out}:
			case <-ctx.Done():
				return
			}
		}
	}()
	// Nothing is left running when each returns.
	defer wg.Wait()
	defer cancel()

	for next := range queue {
		var out pageOut
		select {
		case out = <-next:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !fn(&out) {
			return nil
		}
	}
	return ctx.Err()
}

// Parts returns the parts of the selected pages, one at a time and in
// page order. Several pages are read at a time (Options.Workers), a few
// ahead of the one being handed out, and none is kept after it has been
// handed out.
//
// A part is never an error: a page that cannot be read is a part with
// part.failed. The error is not nil once, at the end, when the parts
// could not all be produced: the context ended, or a limit of
// Options.Limits was reached (ErrLimitExceeded). The part that comes
// with an error is empty.
//
// The parts are those of Document. Stopping the loop early stops the
// reading.
func (f *File) Parts(ctx context.Context) iter.Seq2[gofastcore.Part, error] {
	return func(yield func(gofastcore.Part, error) bool) {
		if f.closed.Load() {
			yield(gofastcore.Part{}, ErrClosed)
			return
		}
		ctx, st := f.begin(ctx)
		defer st.end()
		var limit error
		last, stopped := 0, false
		err := f.each(ctx, want{}, func(out *pageOut) bool {
			if st.budget -= out.decoded; st.budget < 0 {
				limit = fmt.Errorf("%w: the document decodes to more than the limit of %d bytes; what follows page %d is not read",
					gofastcore.ErrLimitExceeded, st.decodeLimit, last)
				return false
			}
			st.finish(out)
			f.note(out)
			last = out.part.Number
			whole := st.charge(&out.part)
			if !yield(out.part, nil) {
				stopped = true
				return false
			}
			if !whole {
				limit = fmt.Errorf("%w: the text and the blocks of the document are more than the limit on text of %d bytes; page %d is cut and what follows it is not read",
					gofastcore.ErrLimitExceeded, st.textLimit, last)
			}
			return whole
		})
		if !stopped && err == nil && limit == nil {
			// What the file holds and shows on no page comes last.
			if part, ok, whole := st.stored(); ok {
				if !yield(part, nil) {
					return
				}
				if !whole {
					limit = fmt.Errorf("%w: the text and the blocks of the document are more than the limit on text of %d bytes; what the file stores is cut",
						gofastcore.ErrLimitExceeded, st.textLimit)
				}
			}
		}
		switch {
		case stopped:
		case err != nil:
			yield(gofastcore.Part{}, err)
		case limit != nil:
			yield(gofastcore.Part{}, limit)
		case f.tooMany:
			yield(gofastcore.Part{}, fmt.Errorf("%w: %d pages were asked for and the limit is %d; the pages after it are not read",
				gofastcore.ErrLimitExceeded, f.asked, f.hi-f.lo))
		}
	}
}

// Page is one page of an open file.
type Page struct {
	f *File
	p *file.Page
}

// Page returns page n of the file, the first being 1. Any page of the
// file can be asked for, whatever range the options select. Nothing is
// read until a method of the page is called.
func (f *File) Page(n int) (*Page, error) {
	if f.closed.Load() {
		return nil, ErrClosed
	}
	if n < 1 || n > len(f.pages) {
		return nil, fmt.Errorf("%w: page %d of a document of %d", gofastcore.ErrNoPartsSelected, n, len(f.pages))
	}
	return &Page{f: f, p: &f.pages[n-1]}, nil
}

// Number returns the number of the page in its file, from 1.
func (p *Page) Number() int { return p.p.Number }

// Part reads the page and returns its part, as Document and Parts give
// it. A page that cannot be read is a part with part.failed, not an
// error; the error is ErrClosed after Close.
//
// A page read by itself knows nothing of the pages before it. With
// Options.Images at ImagesData, each image therefore has its data on
// the first block that shows it on this page, also when an earlier page
// shows it too, where Document and Parts give the data once for the
// document. The limits on decoded bytes and on text apply to the page
// by itself.
func (p *Page) Part() (gofastcore.Part, error) {
	out, err := p.read(want{})
	return out.part, err
}

func (p *Page) read(w want) (pageOut, error) {
	f := p.f
	if f.closed.Load() {
		return pageOut{}, ErrClosed
	}
	out := f.e.page(f.f, p.p, &f.opts, w)
	st := f.newState()
	st.finish(&out)
	st.charge(&out.part)
	f.note(&out)
	return out, nil
}

// Document reads the selected pages and returns the whole document, as
// Extract does: the parts, and with them the document information, the
// outline, the attached files and the warnings of the file.
func (f *File) Document(ctx context.Context) (*gofastcore.Document, error) {
	if f.closed.Load() {
		return nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc := gofastcore.New(Format)
	doc.Source.Name, doc.Source.MIME, doc.Source.Size = f.name, "application/pdf", f.size
	doc.Parts = make([]gofastcore.Part, 0, f.hi-f.lo)

	// The parts are put together in page order, and the budget for
	// decoded bytes is charged in that order, so where a document is cut
	// does not depend on how the workers were scheduled.
	// So is the budget for text, after the titles of the outline.
	ctx, st := f.begin(ctx)
	defer st.end()
	var attachments []attachment
	active, external, incomplete, spent := false, false, false, false
	last, cut := 0, 0
	err := f.each(ctx, want{}, func(out *pageOut) bool {
		if st.budget -= out.decoded; st.budget < 0 {
			spent = true
			return false
		}
		st.finish(out)
		f.note(out)
		last = out.part.Number
		active, external, incomplete = active || out.active, external || out.external, incomplete || out.deepIncomplete
		attachments = append(attachments, out.attachments...)
		whole := st.charge(&out.part)
		if !whole {
			cut = last
		}
		doc.Parts = append(doc.Parts, out.part)
		return whole
	})
	if err != nil {
		return nil, err
	}

	d := f.describe()
	if !spent && cut == 0 {
		if part, ok, whole := st.stored(); ok {
			if !whole {
				cut = part.Number
			}
			doc.Parts = append(doc.Parts, part)
		}
	}
	doc.Metadata, doc.Outline = f.Metadata(), f.Outline()[:st.outline]
	st.attach(ctx, doc, append(append([]attachment(nil), d.attachments...), attachments...))
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	doc.Warnings = append(f.warnings(d.active || active, d.external || external), doc.Warnings...)
	if f.tooMany {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("%d pages were asked for and the limit is %d; the pages after it are not read", f.asked, f.hi-f.lo))
	}
	if spent {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the document decodes to more than the limit of %d bytes; what follows page %d is not read", st.decodeLimit, last))
	}
	if st.attachmentsCut {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the document decodes to more than the limit of %d bytes; some attached files were not extracted", st.decodeLimit))
	}
	if st.imagesCut {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the document decodes to more than the limit of %d bytes; the data of some images is not included", st.decodeLimit))
	}
	if st.outlineCut {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the titles of the outline are more than the limit on text of %d bytes; the outline ends after %d entries", st.textLimit, st.outline))
	}
	if cut > 0 {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the text and the blocks of the document are more than the limit on text of %d bytes; part %d is cut and what follows it is not read", st.textLimit, cut))
	}
	if st.unread {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the limit on text of %d bytes was reached; some attached files were not read", st.textLimit))
	}

	// The reasons that were checked completely. Without DeepHidden, low
	// contrast, covering and clipping are checked in part and are not
	// listed. With it they are, unless a page has more shapes than are
	// followed: then the first two were not checked on all of that page,
	// and are not listed for the document.
	doc.HiddenChecked = f.checked(incomplete)
	if invisibleInMetadata(doc) {
		doc.Warn(gofastcore.SeverityWarning, gofastcore.WarnTextHidden, "the document information or the outline has invisible characters")
	}
	return doc, nil
}
