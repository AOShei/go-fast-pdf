package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// TestLayoutCost: no arrangement of text on a page makes the layout
// take time out of proportion to the size of the page. Each of these
// pages has some hundred thousand pieces of text, arranged so as to
// meet a step of the layout at its worst.
func TestLayoutCost(t *testing.T) {
	if testing.Short() || pdftest.RaceEnabled {
		// The race detector makes everything several times slower, and
		// this is a test of time.
		t.Skip("large pages, timed")
	}
	const n = 120000
	pages := map[string]func(sb *strings.Builder){
		"everything at one point": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0 0 1 100 500 Tm (w%d) Tj\n", i%977)
			}
		},
		"the same word at one point": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				sb.WriteString("1 0 0 1 100 500 Tm (same) Tj\n")
			}
		},
		"single letters along one line": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0 0 1 %d 500 Tm (a) Tj\n", i*9)
			}
		},
		"a grid of single words": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0 0 1 %d %d Tm (cell) Tj\n", 20+(i%300)*40, 20+(i/300)*14)
			}
		},
		"words scattered": func(sb *strings.Builder) {
			x, y := uint32(1), uint32(7)
			for i := 0; i < n; i++ {
				x, y = x*1664525+1013904223, y*22695477+1
				fmt.Fprintf(sb, "1 0 0 1 %d %d Tm (w%d) Tj\n", x%20000, y%20000, i%977)
			}
		},
		"a staircase of blocks": func(sb *strings.Builder) {
			// Each block lower and further right than the last: every
			// cut takes off one block.
			for i := 0; i < 6000; i++ {
				fmt.Fprintf(sb, "1 0 0 1 %d %d Tm (step) Tj\n", 10+i*30, 200000-i*30)
			}
		},
		"lines one under the other": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0 0 1 72 %d Tm (line %d of a long column) Tj\n", 2000000-i*12, i)
			}
		},
		"every piece turned a little": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0.0%d -0.0%d 1 %d %d Tm (turn) Tj\n", i%90, i%90, 72+(i%50)*40, 72+(i/50)*3)
			}
		},
		"hidden and visible by turns": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "%d Tr 1 0 0 1 %d %d Tm (word) Tj\n", (i%2)*3, 72+(i%100)*30, 72+(i/100)*12)
			}
		},
	}
	for name, fill := range pages {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			sb.WriteString("BT /F1 10 Tf\n")
			fill(&sb)
			sb.WriteString("ET")
			data := synthDoc{Font: fontCourier, Pages: []synthPage{{Content: sb.String(), MediaBox: "[0 0 20000 2000000]"}}}.bytes()
			start := time.Now()
			doc := mustExtract(t, data, nil)
			took := time.Since(start)
			words := len(strings.Fields(doc.Text()))
			t.Logf("%d blocks, %d words, %v", len(doc.Parts[0].Blocks), words, took.Round(time.Millisecond))
			if words == 0 {
				t.Error("no text")
			}
			// The contract check takes part of this; the bound is loose
			// on purpose and still far below what a quadratic step needs.
			if took > 20*time.Second {
				t.Errorf("took %v", took)
			}
		})
	}
}

// TestDeepHiddenCost: no arrangement of shapes and text makes the checks
// of DeepHidden take time out of proportion to the size of the page.
// Each page paints 60,000 shapes, which is close to the most that are
// followed, and as many words.
func TestDeepHiddenCost(t *testing.T) {
	if testing.Short() || pdftest.RaceEnabled {
		t.Skip("large pages, timed")
	}
	const n = 60000
	word := func(sb *strings.Builder, i int) {
		fmt.Fprintf(sb, "BT /F1 10 Tf 1 0 0 1 %d %d Tm (w%d) Tj ET\n", 20+(i%100)*50, 20+(i/100)*12, i%977)
	}
	pages := map[string]func(sb *strings.Builder){
		"rectangles as large as the page, then the text": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "0.%d g 0 0 5200 7400 re f\n", i%10)
			}
			for i := 0; i < n; i++ {
				word(sb, i)
			}
		},
		"triangles over half the page, then the text": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "%d 0 m 5000 %d l 0 7000 l f\n", i%100, 3000+i%1000)
			}
			for i := 0; i < n; i++ {
				word(sb, i)
			}
		},
		"small rectangles in one place, and the text on them": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "%d %d 3 3 re f\n", 20+i%40, 20+(i/40)%40)
			}
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "BT /F1 10 Tf 1 0 0 1 %d %d Tm (w%d) Tj ET\n", 20+i%7, 20+i%30, i%977)
			}
		},
		"a rectangle under every word": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 g %d %d 48 11 re f 0.99 g\n", 18+(i%100)*50, 18+(i/100)*12)
				word(sb, i)
			}
		},
		"the text, then rectangles that cover none of it": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				word(sb, i)
			}
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "%d %d 9 9 re f\n", 2500+i%7, 3000+i%5)
			}
		},
		"the text, then rectangles of the width of the page": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				word(sb, i)
			}
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "0 %d 5200 11 re f\n", 21+(i%600)*12)
			}
		},
		"a clip inside a clip for every word": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				if i%2000 == 0 && i > 0 {
					sb.WriteString(strings.Repeat("Q ", 2000) + "\n")
				}
				fmt.Fprintf(sb, "q %d 0 5000 7300 re W n\n", i%2000)
				word(sb, i)
			}
		},
		"strokes across the page between the words": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "0 %d m 5200 %d l S\n", i%7300, (i*7)%7300)
				word(sb, i)
			}
		},
	}
	for name, fill := range pages {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			fill(&sb)
			data := synthDoc{Font: fontCourier, Pages: []synthPage{{Content: sb.String(), MediaBox: "[0 0 5200 7400]"}}}.bytes()
			start := time.Now()
			plain := mustExtract(t, data, nil)
			base := time.Since(start)
			start = time.Now()
			doc := mustExtract(t, data, &gofastcore.Options{DeepHidden: true})
			took := time.Since(start)
			hidden := 0
			for _, b := range doc.Parts[0].Blocks {
				if b.Hidden != 0 {
					hidden++
				}
			}
			t.Logf("%d blocks, %d of them hidden, %v (%v without DeepHidden), warnings %v", len(doc.Parts[0].Blocks), hidden,
				took.Round(time.Millisecond), base.Round(time.Millisecond), codes(doc.Parts[0].Warnings))
			if len(strings.Fields(doc.Text())) != len(strings.Fields(plain.Text())) {
				t.Error("the text is not that of the run without DeepHidden")
			}
			if took > 20*time.Second {
				t.Errorf("took %v", took)
			}
		})
	}
}

// TestImageCost: a block for every image costs what the images cost,
// however many a page has. A page may list a hundred thousand.
func TestImageCost(t *testing.T) {
	if testing.Short() || pdftest.RaceEnabled {
		t.Skip("large pages, timed")
	}
	const n = 100000
	pages := map[string]func(sb *strings.Builder){
		"a grid of images": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "q 9 0 0 9 %d %d cm /Im1 Do Q\n", 10+(i%400)*10, 10+(i/400)*10)
			}
		},
		"all images in one place": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				sb.WriteString("q 9 0 0 9 100 100 cm /Im1 Do Q\n")
			}
		},
		"images among a thousand lines of text": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "q 9 0 0 9 %d %d cm /Im1 Do Q\n", 2000+(i%200)*10, 10+(i/200)*10)
				if i%100 == 0 {
					fmt.Fprintf(sb, "BT /F1 10 Tf 1 0 0 1 72 %d Tm (line %d of the text beside the pictures) Tj ET\n", 10+(i/100)*14, i/100)
				}
			}
		},
	}
	for name, fill := range pages {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			fill(&sb)
			data := synthDoc{Font: fontCourier, Objects: []string{grayImage},
				Pages: []synthPage{{Content: sb.String(), Resources: "/XObject << /Im1 1 0 R >>", MediaBox: "[0 0 5000 15000]"}}}.bytes()
			start := time.Now()
			plain := mustExtract(t, data, &gofastcore.Options{Images: gofastcore.ImagesNone})
			base := time.Since(start)
			start = time.Now()
			doc := mustExtract(t, data, &gofastcore.Options{Images: gofastcore.ImagesMeta})
			took := time.Since(start)
			images := 0
			for _, b := range doc.Parts[0].Blocks {
				if b.Kind == gofastcore.KindImage {
					images++
				}
			}
			t.Logf("%d image blocks among %d blocks, %v (%v without image blocks)", images, len(doc.Parts[0].Blocks), took.Round(time.Millisecond), base.Round(time.Millisecond))
			if images != n || len(doc.Parts[0].Blocks)-images != len(plain.Parts[0].Blocks) {
				t.Errorf("%d image blocks and %d others; without image blocks %d", images, len(doc.Parts[0].Blocks)-images, len(plain.Parts[0].Blocks))
			}
			// The contract check and the JSON round trip of a hundred
			// thousand blocks are most of this; the bound is loose and far
			// below what a step that goes with the square would need.
			if took > 20*time.Second {
				t.Errorf("took %v", took)
			}
		})
	}
}

// TestResourceCost: a page that uses very many named resources takes
// time in proportion to their number. Each name is looked up in its
// dictionary, and a dictionary can list a hundred thousand.
func TestResourceCost(t *testing.T) {
	if testing.Short() || pdftest.RaceEnabled {
		t.Skip("large pages, timed")
	}
	const n = 100000
	// Names of one length that differ at the end: the slowest to tell
	// apart.
	name := func(i int) string { return fmt.Sprintf("NameOfAResourceThatIsLong%07d", i) }
	entries := func(value string) string {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&sb, "/%s %s\n", name(i), value)
		}
		return sb.String()
	}
	pages := map[string]struct {
		resources string
		use       func(sb *strings.Builder, i int)
	}{
		"images": {"/XObject << " + entries("1 0 R") + " >>", func(sb *strings.Builder, i int) {
			fmt.Fprintf(sb, "q 9 0 0 9 %d %d cm /%s Do Q\n", 10+(i%400)*10, 10+(i/400)*10, name(i))
		}},
		"graphics states": {"/ExtGState << " + entries("<< /ca 1 >>") + " >>", func(sb *strings.Builder, i int) {
			fmt.Fprintf(sb, "/%s gs\n", name(i))
		}},
		"colour spaces": {"/ColorSpace << " + entries("/DeviceRGB") + " >>", func(sb *strings.Builder, i int) {
			fmt.Fprintf(sb, "/%s cs\n", name(i))
		}},
		"property lists": {"/Properties << " + entries("<< /MCID 1 >>") + " >>", func(sb *strings.Builder, i int) {
			fmt.Fprintf(sb, "/Span /%s BDC EMC\n", name(i))
		}},
		"fonts that are not there": {"/XObject << " + entries("1 0 R") + " >>", func(sb *strings.Builder, i int) {
			fmt.Fprintf(sb, "BT /%s 10 Tf ET\n", name(i))
		}},
		"names that are not there": {"/XObject << " + entries("1 0 R") + " >> /ExtGState << " + entries("<< /ca 1 >>") + " >>", func(sb *strings.Builder, i int) {
			fmt.Fprintf(sb, "/Missing%d Do /Missing%d gs\n", i, i)
		}},
	}
	for label, p := range pages {
		t.Run(label, func(t *testing.T) {
			var sb strings.Builder
			for i := 0; i < n; i++ {
				p.use(&sb, n-1-i%2*i-(1-i%2)*(n-1-i)) // from both ends by turns
			}
			sb.WriteString("BT /F1 10 Tf 72 72 Td (text) Tj ET")
			data := synthDoc{Font: fontCourier, Objects: []string{grayImage},
				Pages: []synthPage{{Content: sb.String(), Resources: p.resources, MediaBox: "[0 0 5000 5000]"}}}.bytes()
			start := time.Now()
			doc := mustExtract(t, data, &gofastcore.Options{Images: gofastcore.ImagesNone})
			took := time.Since(start)
			t.Logf("%v", took.Round(time.Millisecond))
			if !strings.Contains(doc.Text(), "text") {
				t.Error("the text of the page is not there")
			}
			if took > 5*time.Second {
				t.Errorf("took %v", took)
			}
		})
	}
}

// A large dictionary of resources is searched through an index, a small
// one from its start. Both find the same: the first entry of a name
// that is there twice, and nothing for a name that is not there.
func TestLargeResourceDictionary(t *testing.T) {
	form := func(text string) string {
		return pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 600 800] /Resources << /Font << /F1 5 0 R >> >>",
			"BT /F1 10 Tf 72 700 Td ("+text+") Tj ET")
	}
	for _, others := range []int{0, 5, 500} {
		var dict strings.Builder
		dict.WriteString("/XObject << /Twice 1 0 R ")
		for i := 0; i < others; i++ {
			fmt.Fprintf(&dict, "/Other%d 2 0 R ", i)
		}
		dict.WriteString("/Twice 2 0 R /Last 1 0 R >>")
		data := synthDoc{Font: fontCourier, Objects: []string{form("first"), form("second")},
			Pages: []synthPage{{Content: "/Twice Do /Missing Do q 1 0 0 1 0 -100 cm /Last Do Q", Resources: dict.String()}}}.bytes()
		doc := mustExtract(t, data, nil)
		if got := strings.Join(strings.Fields(doc.Text()), " "); got != "first first" {
			t.Errorf("a dictionary of %d entries: %q, want the first entry of the name twice", others+3, got)
		}
	}
}

// TestOutlineCost: an outline of a hundred thousand items, each of which
// names its destination, in a dictionary of a hundred thousand names,
// is read in time that goes with their number.
func TestOutlineCost(t *testing.T) {
	if testing.Short() || pdftest.RaceEnabled {
		t.Skip("a large outline, timed")
	}
	const n = 100000
	name := func(i int) string { return fmt.Sprintf("DestinationWithALongName%07d", i) }
	objects := []string{fmt.Sprintf("<< /Type /Outlines /First 2 0 R /Last %d 0 R >>", n+1)}
	for i := 0; i < n; i++ {
		next := ""
		if i < n-1 {
			next = fmt.Sprintf("/Next %d 0 R", i+3)
		}
		objects = append(objects, fmt.Sprintf("<< /Title (Item %d) /Parent 1 0 R %s /Dest /%s >>", i, next, name(n-1-i)))
	}
	page := len(objects) + 5
	var dests strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&dests, "/%s [%d 0 R /XYZ 0 %d 0]\n", name(i), page, 100+i%600)
	}
	data := synthDoc{Objects: objects, Catalog: "/Outlines 1 0 R /Dests << " + dests.String() + " >>",
		Pages: []synthPage{{Content: visibleLines}}}.bytes()
	start := time.Now()
	doc := mustExtract(t, data, nil)
	took := time.Since(start)
	t.Logf("%d items, %v", len(doc.Outline), took.Round(time.Millisecond))
	if len(doc.Outline) != n || doc.Outline[n-1].Part != 1 || doc.Outline[0].Y == 0 {
		t.Errorf("%d items, the last %+v", len(doc.Outline), doc.Outline[len(doc.Outline)-1])
	}
	if took > 5*time.Second {
		t.Errorf("took %v", took)
	}
}
