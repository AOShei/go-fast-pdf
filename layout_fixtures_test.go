package gofastpdf_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// Tests on the fixtures of testdata/gen-layout.sh: one text with known
// paragraphs (testdata/src/layout.txt), set in several ways by three
// programs. What is measured is how often the library finds the
// paragraphs and the order of reading that the source has.

// layoutSource returns the title and the paragraphs of the source.
func layoutSource(t testing.TB) (title string, paragraphs []string) {
	t.Helper()
	data, err := os.ReadFile("testdata/src/layout.txt")
	if err != nil {
		t.Fatal(err)
	}
	// A checkout on Windows may have given the file other line ends.
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	parts := strings.Split(strings.TrimSpace(text), "\n\n")
	return parts[0], parts[1:]
}

// boundaries returns where the paragraphs end, as the number of words
// before each end, without the end of the last paragraph.
func boundaries(paragraphs []string) (ends map[int]bool, words int) {
	ends = map[int]bool{}
	for i, p := range paragraphs {
		words += len(pdftest.Words(p))
		if i < len(paragraphs)-1 {
			ends[words] = true
		}
	}
	return ends, words
}

// structure is how well the blocks of a document match the paragraphs
// of its source.
type structure struct {
	// Words is the word-level score of the text against the source, and
	// its Order field the score of the order.
	Words pdftest.Score
	// Found, Extra and Missed count paragraph boundaries: those the
	// source has and the blocks have, those only the blocks have, and
	// those only the source has.
	Found, Extra, Missed int
}

func (s structure) f1() float64 {
	if s.Found == 0 {
		if s.Extra+s.Missed == 0 {
			return 1
		}
		return 0
	}
	p := float64(s.Found) / float64(s.Found+s.Extra)
	r := float64(s.Found) / float64(s.Found+s.Missed)
	return 2 * p * r / (p + r)
}

func measure(t testing.TB, name string, want []string, drop func(text string) bool) structure {
	t.Helper()
	e, err := extractFile(fixture(name), extractOpts{})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var blocks []string
	for _, b := range e.Doc.AllBlocks() {
		if drop == nil || !drop(b.Text) {
			blocks = append(blocks, b.Text)
		}
	}
	var s structure
	s.Words = pdftest.Compare(strings.Join(blocks, "\n\n"), strings.Join(want, "\n\n"))
	got, _ := boundaries(blocks)
	wanted, _ := boundaries(want)
	for at := range got {
		if wanted[at] {
			s.Found++
		} else {
			s.Extra++
		}
	}
	s.Missed = len(wanted) - s.Found
	return s
}

// TestParagraphFixtures: the paragraphs of the source are the blocks,
// in each way of setting paragraphs and from each program.
func TestParagraphFixtures(t *testing.T) {
	title, paragraphs := layoutSource(t)
	want := append([]string{title}, paragraphs...)
	var found, extra, missed int
	for _, style := range []string{"spaced", "indented", "justified"} {
		for _, producer := range []string{"libreoffice", "firefox", "groff"} {
			name := "layout-" + style + "-" + producer
			s := measure(t, name, want, nil)
			t.Logf("%-32s words F1 %.4f, order %.4f, paragraph ends: %d found, %d extra, %d missed, F1 %.3f",
				name, s.Words.F1, s.Words.Order, s.Found, s.Extra, s.Missed, s.f1())
			if s.Words.F1 < 0.98 || s.Words.Order < 0.98 {
				t.Errorf("%s: words F1 %.4f, order %.4f", name, s.Words.F1, s.Words.Order)
			}
			if s.f1() < 0.95 {
				t.Errorf("%s: paragraph ends F1 %.3f, want at least 0.95", name, s.f1())
			}
			found, extra, missed = found+s.Found, extra+s.Extra, missed+s.Missed
		}
	}
	all := structure{Found: found, Extra: extra, Missed: missed}
	t.Logf("all nine: %d paragraph ends found, %d extra, %d missed, F1 %.3f", found, extra, missed, all.f1())
}

// TestReadingOrderFixtures: text in columns is read column by column,
// a title over columns first, a box beside the text in one piece.
func TestReadingOrderFixtures(t *testing.T) {
	title, paragraphs := layoutSource(t)
	const boxText = "Opening hours change during the yearly count."
	for _, c := range []struct {
		name  string
		title bool
		box   bool
	}{
		{"layout-cols2-firefox", false, false},
		{"layout-cols2-groff", false, false},
		{"layout-cols3-firefox", false, false},
		{"layout-cols3-groff", false, false},
		{"layout-title2-firefox", true, false},
		{"layout-title2-groff", true, false},
		{"layout-sidebox-firefox", true, true},
	} {
		want := paragraphs
		if c.title {
			want = append([]string{title}, paragraphs...)
		}
		var drop func(string) bool
		if c.box {
			// The box stands beside the first paragraphs; where its text
			// comes among theirs is open. It must be there, in one
			// piece, and the rest in the order of the source.
			drop = func(text string) bool { return text == boxText }
			e, err := extractFile(fixture(c.name), extractOpts{})
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, b := range e.Doc.AllBlocks() {
				if b.Text == boxText {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%s: the text of the box is in %d blocks of its own", c.name, n)
			}
		}
		s := measure(t, c.name, want, drop)
		// A paragraph that runs from the foot of one column to the head
		// of the next is two blocks: those ends are extra.
		t.Logf("%-28s words F1 %.4f, order %.4f, paragraph ends: %d found, %d extra, %d missed",
			c.name, s.Words.F1, s.Words.Order, s.Found, s.Extra, s.Missed)
		if s.Words.F1 < 0.98 || s.Words.Order < 0.98 {
			t.Errorf("%s: words F1 %.4f, order %.4f; want both at least 0.98", c.name, s.Words.F1, s.Words.Order)
		}
		if s.Missed > 0 {
			t.Errorf("%s: %d paragraph ends of the source are not block ends", c.name, s.Missed)
		}
	}
}

// TestSpacedHeadingFixtures: headings set with their letters apart, by
// a browser and by an office suite, are read as their words.
func TestSpacedHeadingFixtures(t *testing.T) {
	_, paragraphs := layoutSource(t)
	want := []string{"CONTENTS", "ANNUAL REPORT", paragraphs[0], "Summary of the year", paragraphs[1]}
	for _, producer := range []string{"firefox", "libreoffice"} {
		e, err := extractFile(fixture("layout-tracking-"+producer), extractOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if got := texts(&e.Doc.Parts[0]); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: blocks\n  %s", producer, strings.Join(got, "\n  "))
		}
	}
}

// TestAttachedFixture: files that qpdf attached to a document.
func TestAttachedFixture(t *testing.T) {
	data, err := os.ReadFile(fixture("attached"))
	if err != nil {
		t.Fatal(err)
	}
	child := func(ctx context.Context, name, mime string, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
		if mime != "application/pdf" {
			return nil, fmt.Errorf("%w: %s", gofastcore.ErrUnsupported, mime)
		}
		return gofastpdf.Extract(ctx, r, size, opts)
	}
	doc := mustExtract(t, data, &gofastcore.Options{Child: child})
	if len(doc.Children) != 2 {
		t.Fatalf("%d children", len(doc.Children))
	}
	pdf, txt := doc.Children[0], doc.Children[1]
	if pdf.Source.Name != "latin1.pdf" || pdf.Source.Format != "pdf" || len(pdf.Parts) != 1 || !strings.Contains(pdf.Text(), "Größe") {
		t.Errorf("latin1.pdf: %+v, %d parts", pdf.Source, len(pdf.Parts))
	}
	if _, ok := warning(txt.Warnings, gofastcore.WarnChildUnsupported); !ok || txt.Source.Name != "layout.txt" || txt.Source.MIME != "text/plain" || txt.Source.Size == 0 {
		t.Errorf("layout.txt: %+v, %+v", txt.Source, txt.Warnings)
	}
	// The document itself reads as the file it was made from.
	base, err := extractFile(fixture("english"), extractOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Text() != base.Doc.Text() {
		t.Error("the text differs from that of english.pdf")
	}
}
