package gofastpdf_test

import (
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// linesOf opens data and returns the lines and the part of its page n.
func linesOf(t testing.TB, data []byte, opts *gofastcore.Options, n int) ([]gofastpdf.Line, gofastcore.Part) {
	t.Helper()
	f, err := gofastpdf.OpenBytes(data, opts)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.Page(n)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := page.Lines()
	if err != nil {
		t.Fatal(err)
	}
	part, err := page.Part()
	if err != nil {
		t.Fatal(err)
	}
	return lines, part
}

func lettersOf(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// TestLinesBelongToTheirBlocks: on every page of the fixtures and of
// the synthetic documents, the lines are those of the blocks of the
// page's part: in the order of the blocks, with their hidden reasons,
// and with all their letters; the words of a line are its text, and
// they lie in its box.
func TestLinesBelongToTheirBlocks(t *testing.T) {
	lines, words := 0, 0
	for name, data := range equivalenceDocs(t) {
		for _, opts := range []gofastcore.Options{{}, {Images: gofastcore.ImagesMeta, DeepHidden: true}} {
			f, err := gofastpdf.OpenBytes(data, &opts)
			if err != nil {
				t.Fatal(err)
			}
			for n := 1; n <= f.NumPages(); n++ {
				got, part := linesOf(t, data, &opts, n)
				where := fmt.Sprintf("%s, page %d", name, n)
				perBlock := map[int]string{}
				last := -1
				for _, l := range got {
					lines++
					if k := part.Blocks[min(max(l.Block, 0), len(part.Blocks)-1)].Kind; l.Block < last || l.Block >= len(part.Blocks) || k != gofastcore.KindParagraph && k != gofastcore.KindHeading && k != gofastcore.KindListItem && !k.Furniture() {
						t.Fatalf("%s: a line of block %d after one of block %d, of %d blocks", where, l.Block, last, len(part.Blocks))
					}
					last = l.Block
					block := &part.Blocks[l.Block]
					if l.Hidden != block.Hidden {
						t.Errorf("%s: line %q is %q and its block %q", where, l.Text, l.Hidden, block.Hidden)
					}
					perBlock[l.Block] += lettersOf(l.Text)
					var texts []string
					for _, w := range l.Words {
						words++
						texts = append(texts, w.Text)
						const slack = 1.0
						if b, in := l.BBox, w.BBox; in.X0 < b.X0-slack || in.Y0 < b.Y0-slack || in.X1 > b.X1+slack || in.Y1 > b.Y1+slack || in.X1 < in.X0 || in.Y1 < in.Y0 {
							t.Errorf("%s: the word %q at %+v is not in its line %q at %+v", where, w.Text, in, l.Text, b)
						}
					}
					if strings.Join(texts, " ") != strings.Join(strings.Fields(l.Text), " ") {
						t.Errorf("%s: line %q has the words %q", where, l.Text, texts)
					}
					if l.Size <= 0 || l.BBox.X1 < l.BBox.X0 {
						t.Errorf("%s: line %q has size %v and box %+v", where, l.Text, l.Size, l.BBox)
					}
					// A line lies in the box of its block.
					if b, in := block.Loc.BBox, l.BBox; in.X0 < b.X0-0.01 || in.Y0 < b.Y0-0.01 || in.X1 > b.X1+0.01 || in.Y1 > b.Y1+0.01 {
						t.Errorf("%s: line %q at %+v is not in its block at %+v", where, l.Text, in, b)
					}
				}
				for i := range part.Blocks {
					// The marker of an item or a heading is on its first line.
					if b := &part.Blocks[i]; (b.Kind == gofastcore.KindParagraph || b.Kind == gofastcore.KindListItem) && perBlock[i] != lettersOf(b.Marker+b.Text) {
						t.Errorf("%s: block %d is %.60q and its lines have the letters %.60q", where, i, b.Text, perBlock[i])
					}
				}
			}
		}
	}
	if lines < 2000 || words < 20000 {
		t.Errorf("%d lines and %d words looked at", lines, words)
	}
}

// TestWordBoxes: where each word is, on pages set in a font whose
// characters are all 0.6 of its size wide, so that the places are known.
func TestWordBoxes(t *testing.T) {
	type word struct {
		text   string
		x0, x1 float64
	}
	cases := []struct {
		name, content string
		want          []word
	}{
		{"one string", "BT /F1 10 Tf 100 700 Td (ab cd) Tj ET", []word{{"ab", 100, 112}, {"cd", 118, 130}}},
		{"two spaces", "BT /F1 10 Tf 100 700 Td (ab  cd) Tj ET", []word{{"ab", 100, 112}, {"cd", 124, 136}}},
		{"word spacing", "BT /F1 10 Tf 5 Tw 100 700 Td (ab cd) Tj ET", []word{{"ab", 100, 112}, {"cd", 123, 135}}},
		{"character spacing", "BT /F1 10 Tf 1 Tc 100 700 Td (ab cd) Tj ET", []word{{"ab", 100, 113}, {"cd", 121, 134}}},
		{"horizontal scaling", "BT /F1 10 Tf 200 Tz 100 700 Td (ab cd) Tj ET", []word{{"ab", 100, 124}, {"cd", 136, 160}}},
		{"kerning in a word", "BT /F1 10 Tf 100 700 Td [(a) -100 (b cd)] TJ ET", []word{{"ab", 100, 113}, {"cd", 119, 131}}},
		{"a gap for a space", "BT /F1 10 Tf 100 700 Td [(ab) -400 (cd)] TJ ET", []word{{"ab", 100, 112}, {"cd", 116, 128}}},
		{"a kern after the space", "BT /F1 10 Tf 100 700 Td [(ab ) -300 (cd)] TJ ET", []word{{"ab", 100, 112}, {"cd", 121, 133}}},
		{"a kern before the space", "BT /F1 10 Tf 100 700 Td [(ab) -100 ( cd)] TJ ET", []word{{"ab", 100, 112}, {"cd", 119, 131}}},
		{"a space set by itself at the margin", "BT /F1 10 Tf 100 700 Td (ab cd) Tj ET BT /F1 10 Tf 131 700 Td ( ) Tj ET", []word{{"ab", 100, 112}, {"cd", 118, 130}}},
		{"words drawn one by one", "BT /F1 10 Tf 100 700 Td (ab) Tj 18 0 Td (cd) Tj 18 0 Td (ef) Tj ET", []word{{"ab", 100, 112}, {"cd", 118, 130}, {"ef", 136, 148}}},
		{"a tab", "BT /F1 10 Tf 100 700 Td (ab) Tj 100 0 Td (cd) Tj ET", []word{{"ab", 100, 112}, {"cd", 200, 212}}},
		{"a larger size by the text matrix", "BT /F1 10 Tf 2 0 0 2 100 700 Tm (ab cd) Tj ET", []word{{"ab", 100, 124}, {"cd", 136, 160}}},
		{"a line drawn in two pieces at different times",
			"BT /F1 10 Tf 136 700 Td (ef) Tj ET BT /F1 10 Tf 100 600 Td (other) Tj ET BT /F1 10 Tf 100 700 Td (ab cd) Tj ET",
			[]word{{"ab", 100, 112}, {"cd", 118, 130}, {"ef", 136, 148}, {"other", 100, 130}}},
		{"letters set apart", "BT /F1 10 Tf 6 Tc 100 700 Td (HEADING) Tj ET   BT /F1 10 Tf 0 Tc 100 680 Td (some more text below it) Tj ET",
			[]word{{"HEADING", 100, 178}, {"some", 100, 124}, {"more", 130, 154}, {"text", 160, 184}, {"below", 190, 220}, {"it", 226, 238}}},
		{"text that replaces its glyphs", "/Span << /ActualText (ab cd) >> BDC BT /F1 10 Tf 100 700 Td (xxxxxxxxxx) Tj ET EMC",
			[]word{{"ab", 100, 124}, {"cd", 136, 160}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := synthDoc{Font: fontCourier, Pages: []synthPage{{Content: c.content}}}
			lines, _ := linesOf(t, d.bytes(), nil, 1)
			var got []word
			for _, l := range lines {
				if l.Font != "Courier" || math.Abs(l.Size-10) > 0.01 && math.Abs(l.Size-20) > 0.01 {
					t.Errorf("line %q in %q at %v", l.Text, l.Font, l.Size)
				}
				for _, w := range l.Words {
					got = append(got, word{w.Text, w.BBox.X0, w.BBox.X1})
				}
			}
			if len(got) != len(c.want) {
				t.Fatalf("words %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i].text != c.want[i].text || math.Abs(got[i].x0-c.want[i].x0) > 0.01 || math.Abs(got[i].x1-c.want[i].x1) > 0.01 {
					t.Errorf("word %d is %v, want %v", i, got[i], c.want[i])
				}
			}
		})
	}

	// The boxes are in the space of the part: on a page that is turned,
	// a line that runs across the sheet runs down the page.
	d := synthDoc{Font: fontCourier, Pages: []synthPage{{Content: "BT /F1 10 Tf 100 700 Td (ab cd) Tj ET", Extra: "/Rotate 90"}}}
	lines, part := linesOf(t, d.bytes(), nil, 1)
	if len(lines) != 1 || len(lines[0].Words) != 2 {
		t.Fatalf("turned: %+v", lines)
	}
	ab, cd := lines[0].Words[0].BBox, lines[0].Words[1].BBox
	if math.Abs(ab.Y0-100) > 0.01 || math.Abs(ab.Y1-112) > 0.01 || math.Abs(cd.Y0-118) > 0.01 || math.Abs(cd.Y1-130) > 0.01 || ab.X0 != cd.X0 || lines[0].BBox != part.Blocks[0].Loc.BBox {
		t.Errorf("turned: ab at %+v, cd at %+v, the line at %+v and its block at %+v", ab, cd, lines[0].BBox, part.Blocks[0].Loc.BBox)
	}

	// A line that a reader cannot see says so, and a line of a page that
	// cannot be read is no line.
	d = synthDoc{Pages: []synthPage{{Content: visibleLines + "BT /F1 12 Tf 3 Tr 72 500 Td (secret words) Tj ET"}}}
	lines, _ = linesOf(t, d.bytes(), nil, 1)
	if n := len(lines); n != 4 || lines[3].Hidden != gofastcore.HiddenInvisible || lines[3].Text != "secret words" || lines[0].Hidden != 0 || lines[0].Font != "Helvetica" {
		t.Errorf("hidden: %+v", lines)
	}
	if lines, part := linesOf(t, damagedMiddlePage(), nil, 2); len(lines) != 0 || !part.Has(gofastcore.WarnPartFailed) {
		t.Errorf("a page that cannot be read: %d lines, %v", len(lines), codes(part.Warnings))
	}
}

// TestWordBoxesAgainstPdftotext compares the words of the fixtures and
// where they are with what pdftotext -bbox says, which works them out
// from the same widths in its own way.
func TestWordBoxesAgainstPdftotext(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	type refWord struct {
		XMin float64 `xml:"xMin,attr"`
		XMax float64 `xml:"xMax,attr"`
		YMin float64 `xml:"yMin,attr"`
		YMax float64 `xml:"yMax,attr"`
		Text string  `xml:",chardata"`
	}
	var doc struct {
		Pages []struct {
			Words []refWord `xml:"word"`
		} `xml:"body>doc>page"`
	}
	total, placed := 0, 0
	for _, name := range []string{
		"english", "layout-justified-libreoffice", "layout-spaced-firefox", "layout-indented-groff",
		"layout-cols2-firefox", "layout-cols3-groff", "para-de-libreoffice", "para-el-firefox",
	} {
		path := filepath.Join("testdata", "fixtures", name+".pdf")
		out, err := exec.Command("pdftotext", "-bbox", path, "-").Output()
		if err != nil && total == 0 {
			// Poppler's pdftotext has -bbox. Another program of the same
			// name, which some systems have, does not.
			t.Skipf("this pdftotext does not write word boxes: %v", err)
		}
		if err != nil {
			t.Fatalf("%s: pdftotext: %v", name, err)
		}
		doc.Pages = nil
		if err := xml.Unmarshal(out, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		data, _ := os.ReadFile(path)
		fileTotal, filePlaced := 0, 0
		for n := range doc.Pages {
			// The reference words of the page, by their text.
			ref := map[string][]refWord{}
			for _, w := range doc.Pages[n].Words {
				ref[w.Text] = append(ref[w.Text], w)
			}
			lines, _ := linesOf(t, data, nil, n+1)
			for _, l := range lines {
				for _, w := range l.Words {
					fileTotal++
					// The same word in the same place, to within a point
					// along the line and within the height of the line.
					for _, r := range ref[w.Text] {
						if math.Abs(r.XMin-w.BBox.X0) <= 1 && math.Abs(r.XMax-w.BBox.X1) <= 1 && r.YMin < w.BBox.Y1 && r.YMax > w.BBox.Y0 {
							filePlaced++
							break
						}
					}
				}
			}
		}
		t.Logf("%-32s %5d words, %5d where pdftotext has them", name, fileTotal, filePlaced)
		if float64(filePlaced) < 0.97*float64(fileTotal) {
			t.Errorf("%s: %d of %d words are where pdftotext has them", name, filePlaced, fileTotal)
		}
		total, placed = total+fileTotal, placed+filePlaced
	}
	if total < 2500 {
		t.Errorf("%d words compared", total)
	}
	t.Logf("%d of %d words are where pdftotext has them (%.2f%%)", placed, total, 100*float64(placed)/float64(total))
}
