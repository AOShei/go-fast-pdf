package gofastpdf_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// Three paragraphs for the layout tests, as the lines they are set in.
// In Courier 10 a character is 6 points wide, so the longest of these
// lines is 192 points.
var (
	paraA = []string{"The committee met on Tuesday to", "review the budget for next year", "and agreed on most of it."}
	paraB = []string{"A second meeting will be held in", "the spring to settle the points", "that remain open."}
	paraC = []string{"Members who cannot attend may", "send their remarks in writing", "before the end of March."}
)

func joined(lines []string) string { return strings.Join(lines, " ") }

func cat(parts ...[]textLine) []textLine {
	var out []textLine
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestParagraphs: which lines form a paragraph.
func TestParagraphs(t *testing.T) {
	a, b := joined(paraA), joined(paraB)
	for _, tc := range []struct {
		name    string
		content string
		want    []string
	}{
		{"one paragraph", draw(10, stack(72, 700, 12, paraA...)...), []string{a}},
		{"a space between two paragraphs",
			draw(10, cat(stack(72, 700, 12, paraA...), stack(72, 656, 12, paraB...))...), []string{a, b}},
		{"a small space between two paragraphs",
			draw(10, cat(stack(72, 700, 12, paraA...), stack(72, 660, 12, paraB...))...), []string{a, b}},
		{"first lines indented, no space between",
			draw(10, textLine{90, 700, paraA[0]}, textLine{72, 688, paraA[1]}, textLine{72, 676, paraA[2]},
				textLine{90, 664, paraB[0]}, textLine{72, 652, paraB[1]}, textLine{72, 640, paraB[2]}), []string{a, b}},
		{"hanging indents, no space between",
			draw(10, textLine{72, 700, "1. " + paraA[0]}, textLine{90, 688, paraA[1]}, textLine{90, 676, paraA[2]},
				textLine{72, 664, "2. " + paraB[0]}, textLine{90, 652, paraB[1]}, textLine{90, 640, paraB[2]}), []string{"1. " + a, "2. " + b}},
		{"a short last line, then a new paragraph with no space and no indent",
			draw(10, cat(stack(72, 700, 12, paraB...), stack(72, 664, 12, paraA...))...), []string{b, a}},
		{"lines centred on one axis",
			draw(10, textLine{200 - 3*20, 700, "Minutes of the meeting"[:20]}, textLine{200 - 3*14, 688, "of the committee"[:14]}, textLine{200 - 3*8, 676, "of 2024."}),
			[]string{"Minutes of the meeti of the committ of 2024."}},
		{"a heading in a larger size",
			"BT /F1 16 Tf 1 0 0 1 72 720 Tm (The annual meeting) Tj ET\n" + draw(10, stack(72, 700, 12, paraA...)...),
			[]string{"The annual meeting", a}},
		{"double spacing",
			draw(10, stack(72, 700, 24, append(append([]string{}, paraA[:2]...), paraC[:2]...)...)...),
			[]string{paraA[0] + " " + paraA[1] + " " + paraC[0] + " " + paraC[1]}},
		{"double spacing with a wider space between two paragraphs",
			draw(10, cat(stack(72, 700, 24, paraA...), stack(72, 700-3*24-14, 24, paraB...))...), []string{a, b}},
		{"two columns under one line",
			draw(10, cat([]textLine{{72, 700, "Minutes of the meeting of the committee on the budget of the year"}},
				stack(72, 688, 12, paraA...), stack(300, 688, 12, paraB...))...),
			[]string{"Minutes of the meeting of the committee on the budget of the year", a, b}},
		{"lines far apart", draw(10, stack(72, 700, 40, paraA...)...), paraA},
		{"single lines of different length, as in a list",
			draw(10, stack(72, 700, 12, "Bread", "Two bottles of milk", "Eggs", "A newspaper for the weekend")...),
			[]string{"Bread", "Two bottles of milk", "Eggs", "A newspaper for the weekend"}},
		{"entries of a table of contents",
			draw(10, stack(72, 700, 12, "Preface .................. iv", "Introduction .............. 1", "The history of the library and", "of its people ............. 3", "Annex A ................. A-1")...),
			[]string{"Preface .................. iv", "Introduction .............. 1", "The history of the library and of its people ............. 3", "Annex A ................. A-1"}},
		{"a sentence with an ellipsis and a number at the end of a line",
			draw(10, stack(72, 700, 12, "The committee met and met ... 3", "times before it could agree on", "most of it.")...),
			[]string{"The committee met and met ... 3 times before it could agree on most of it."}},
		// The room at the end of a line is held against the width of the
		// next line's first word, which is measured in characters: a per
		// mille sign is three bytes and an O with a stroke two, and neither
		// is wider for it. Here the room is of eight characters.
		{"room for a first word of five signs of three bytes",
			draw(10, stack(72, 700, 12, "The committee met on a Tuesday", "to speak of the budget", "\xbd\xbd\xbd\xbd\xbd and then of the rest")...),
			[]string{"The committee met on a Tuesday to speak of the budget", "‰‰‰‰‰ and then of the rest"}},
		{"no room for a first word, before letters of two bytes",
			draw(10, stack(72, 700, 12, "The committee met on a Tuesday", "to speak of the budget", "tomorrow \xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9")...),
			[]string{"The committee met on a Tuesday to speak of the budget tomorrow ØØØØØØØØØØØØØØØ"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := blockTexts(t, tc.content); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("blocks:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}

	t.Run("a heading in another font", func(t *testing.T) {
		d := synthDoc{Font: fontCourier, Pages: []synthPage{{
			Fonts:   "/F2 << /Type /Font /Subtype /Type1 /BaseFont /Courier-Bold >>",
			Content: "BT /F2 10 Tf 1 0 0 1 72 712 Tm (The annual meeting of the year) Tj ET\n" + draw(10, stack(72, 700, 12, paraA...)...),
		}}}
		if got := texts(&mustExtract(t, d.bytes(), nil).Parts[0]); !reflect.DeepEqual(got, []string{"The annual meeting of the year", a}) {
			t.Errorf("blocks %q", got)
		}
	})
	t.Run("words in another font inside a paragraph", func(t *testing.T) {
		// A line that ends in bold and one that starts in bold are still
		// one paragraph.
		d := synthDoc{Font: fontCourier, Pages: []synthPage{{
			Fonts: "/F2 << /Type /Font /Subtype /Type1 /BaseFont /Courier-Bold >>",
			Content: "BT /F1 10 Tf 1 0 0 1 72 700 Tm (The committee met on ) Tj /F2 10 Tf (Tuesday to) Tj\n" +
				"1 0 0 1 72 688 Tm (review the budget) Tj /F1 10 Tf ( for next year) Tj\n" +
				"1 0 0 1 72 676 Tm (and agreed on most of it.) Tj ET",
		}}}
		if got := texts(&mustExtract(t, d.bytes(), nil).Parts[0]); !reflect.DeepEqual(got, []string{a}) {
			t.Errorf("blocks %q", got)
		}
	})
	// A line in another font than the line above it is a heading when the
	// fonts are of two typefaces, and not when a typeface is embedded twice.
	widths := strings.Repeat(" 600", 95)
	subset := func(name, descriptor string) string {
		return fmt.Sprintf("<< /Type /Font /Subtype /TrueType /BaseFont /%s /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 126 /Widths [%s] /FontDescriptor << /Type /FontDescriptor /FontName /%s %s >> >>",
			name, widths, name, descriptor)
	}
	regular := subset("ABCDEF+Courier", "/Flags 32")
	for _, tc := range []struct {
		name, first, second string
		want                []string
	}{
		{"two subsets of one typeface", regular, subset("GHIJKL+Courier", "/Flags 32"), []string{a}},
		{"a typeface embedded as a subset and whole", regular, subset("Courier", "/Flags 32"), []string{a}},
		{"one typeface, twice under one name", regular, regular, []string{a}},
		{"two typefaces with one tag", regular, subset("ABCDEF+Prestige", "/Flags 32"), []string{paraA[0], joined(paraA[1:])}},
		{"a name that starts with no tag", subset("Abcdef+Courier", "/Flags 32"), subset("Ghijkl+Courier", "/Flags 32"), []string{paraA[0], joined(paraA[1:])}},
		{"one name, the first bold by its flags", subset("ABCDEF+Courier", "/Flags 262176"), subset("GHIJKL+Courier", "/Flags 32"), []string{paraA[0], joined(paraA[1:])}},
		{"one name, the first slanted by its angle", subset("ABCDEF+Courier", "/Flags 32 /ItalicAngle -12"), subset("GHIJKL+Courier", "/Flags 32"), []string{paraA[0], joined(paraA[1:])}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := synthDoc{Font: fontCourier, Pages: []synthPage{{
				Fonts: "/F2 " + tc.first + " /F3 " + tc.second,
				Content: "BT /F2 10 Tf 1 0 0 1 72 700 Tm (" + paraA[0] + ") Tj /F3 10 Tf\n" +
					"1 0 0 1 72 688 Tm (" + paraA[1] + ") Tj 1 0 0 1 72 676 Tm (" + paraA[2] + ") Tj ET",
			}}}
			if got := texts(&mustExtract(t, d.bytes(), nil).Parts[0]); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("blocks %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTextConventions: how lines become the text of a block.
func TestTextConventions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    []string
	}{
		{"lines are joined with one space",
			draw(10, stack(72, 700, 12, "The first line ends here ", " and the second begins.")...),
			[]string{"The first line ends here and the second begins."}},
		{"a hyphen at the end of a line stays and the word is not split",
			draw(10, stack(72, 700, 12, "This is a matter of inter-", "national law and of well-", "known rules, see pages 10-", "12 of the report.")...),
			[]string{"This is a matter of inter-national law and of well-known rules, see pages 10-12 of the report."}},
		{"a dash at the end of a line is not a hyphen",
			draw(10, stack(72, 700, 12, "The answer was short -", "it was no, as before.")...),
			[]string{"The answer was short - it was no, as before."}},
		// In the font of this test the code 1 is a soft hyphen.
		{"a soft hyphen at the end of a line goes",
			draw(10, stack(72, 700, 12, "This is a matter of inter\x01", "national law and order.")...),
			[]string{"This is a matter of international law and order."}},
		{"a soft hyphen at the end of a line stays when no word continues",
			draw(10, stack(72, 700, 12, "As the report says, inter\x01", "(see above) and \x01", "national.")...),
			[]string{"As the report says, inter\u00ad (see above) and \u00ad national."}},
		{"a soft hyphen inside a line stays",
			draw(10, textLine{72, 700, "A matter of inter\x01national law."}),
			[]string{"A matter of inter\u00adnational law."}},
		{"the ends are trimmed",
			draw(10, textLine{72, 700, "   Padded with spaces.   "}), []string{"Padded with spaces."}},
		{"spaces inside a line are kept",
			draw(10, textLine{72, 700, "One.  Two spaces after a full stop."}), []string{"One.  Two spaces after a full stop."}},
		{"a no-break space at the end is white space",
			draw(10, textLine{72, 700, "Ends with a no-break space.\xa0"}), []string{"Ends with a no-break space."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmap := "1 begincodespacerange <00> <FF> endcodespacerange 1 beginbfchar <01> <00AD> endbfchar"
			d := synthDoc{
				Objects: []string{pdftest.Stream("", cmap)},
				Font:    "<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding /ToUnicode 1 0 R >>",
				Pages:   []synthPage{{Content: tc.content}},
			}
			if got := texts(&mustExtract(t, d.bytes(), nil).Parts[0]); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("blocks %q\nwant   %q", got, tc.want)
			}
		})
	}

	t.Run("what a font can say a glyph is", func(t *testing.T) {
		// A ToUnicode map may give any character: a ligature, a tab, a
		// line break, a control character.
		cmap := "1 begincodespacerange <00> <FF> endcodespacerange 6 beginbfchar " +
			"<41> <FB01> <42> <0009> <43> <000A> <44> <0007> <45> <000D> <46> <0085> endbfchar"
		d := synthDoc{
			Objects: []string{pdftest.Stream("", cmap)},
			Font:    "<< /Type /Font /Subtype /Type1 /BaseFont /Courier /ToUnicode 1 0 R >>",
			Pages:   []synthPage{{Content: draw(10, textLine{72, 700, "aAbBcCdDeEfFg"})}},
		}
		part := &mustExtract(t, d.bytes(), nil).Parts[0]
		// A ligature is its letters; white space of any kind is a space
		// inside a line; a control character is U+FFFD.
		if got := texts(part); !reflect.DeepEqual(got, []string{"afib c d�e f g"}) {
			t.Errorf("blocks %q", got)
		}
		if w, ok := warning(part.Warnings, gofastcore.WarnTextUnmappedGlyphs); !ok || w.Severity != gofastcore.SeverityWarning {
			t.Errorf("warnings %+v", part.Warnings)
		}
	})

	t.Run("glyphs without a mapping", func(t *testing.T) {
		d := synthDoc{
			Font:  "<< /Type /Font /Subtype /Type1 /BaseFont /Subset /FontDescriptor << /Type /FontDescriptor /FontFile3 3 0 R >> /Encoding << /Differences [200 /glyphone /glyphtwo] >> >>",
			Pages: []synthPage{{Content: draw(10, textLine{72, 700, "ok \310\311 ok"})}},
		}
		part := &mustExtract(t, d.bytes(), nil).Parts[0]
		if got := texts(part); len(got) != 1 || !strings.Contains(got[0], "�") {
			t.Errorf("blocks %q", got)
		}
		if !part.Has(gofastcore.WarnTextUnmappedGlyphs) {
			t.Errorf("warnings %v", codes(part.Warnings))
		}
	})

	t.Run("text written from right to left", func(t *testing.T) {
		// A, B and C are shin, lamed and final mem: the word shalem, read
		// from right to left. However a page draws it, it is read so.
		cmap := "1 begincodespacerange <00> <FF> endcodespacerange 7 beginbfchar <41> <05E9> <42> <05DC> <43> <05DD> <28> <0028> <29> <0029> <44> <05D0> <45> <05D1> endbfchar"
		one := func(x int, s string) string { return fmt.Sprintf("BT /F1 10 Tf %d 700 Td (%s) Tj ET\n", x, s) }
		for _, c := range []struct{ name, content, want string }{
			{"drawn as it is seen", draw(10, textLine{72, 700, "CBA"}), "\u05e9\u05dc\u05dd"},
			{"drawn glyph by glyph from right to left", one(84, "A") + one(78, "B") + one(72, "C"), "\u05e9\u05dc\u05dd"},
			{"with a number, read from left to right", draw(10, textLine{72, 700, "12 CBA"}), "\u05e9\u05dc\u05dd 12"},
			{"two words, the second drawn first", one(72, "ED") + one(96, "CBA"), "\u05e9\u05dc\u05dd \u05d0\u05d1"},
			{"a Latin word inside", draw(10, textLine{72, 700, "CBA abc ED"}), "\u05d0\u05d1 abc \u05e9\u05dc\u05dd"},
		} {
			d := synthDoc{
				Objects: []string{pdftest.Stream("", cmap)},
				Font:    "<< /Type /Font /Subtype /Type1 /BaseFont /Courier /ToUnicode 1 0 R >>",
				Pages:   []synthPage{{Content: c.content}},
			}
			doc := mustExtract(t, d.bytes(), nil)
			if got := texts(&doc.Parts[0]); len(got) != 1 || got[0] != c.want {
				t.Errorf("%s: %q, want %q", c.name, got, c.want)
			}
			if len(doc.Parts[0].Warnings) != 0 {
				t.Errorf("%s: warnings %v", c.name, doc.Parts[0].Warnings)
			}
		}
	})
}

// TestTabs: a gap much wider than a space, between two pieces of a
// line, is a tab, and lines with tabs are rows.
func TestTabs(t *testing.T) {
	row := func(y float64, cells ...string) string {
		// The cells are set 150 points apart in one line of the stream.
		var sb strings.Builder
		fmt.Fprintf(&sb, "BT /F1 10 Tf 1 0 0 1 72 %g Tm ", y)
		for i, c := range cells {
			if i > 0 {
				sb.WriteString("150 0 Td ")
			}
			fmt.Fprintf(&sb, "(%s) Tj ", c)
		}
		sb.WriteString("ET\n")
		return sb.String()
	}
	for _, tc := range []struct {
		name    string
		content string
		want    []string
	}{
		{"a label and its value", row(700, "Name:", "Jane Doe"), []string{"Name:\tJane Doe"}},
		// Three rows of three cells are a table found by their alignment
		// (aligned_test.go), with the cells the tabs would give.
		{"rows of a table",
			row(700, "Item", "Price", "Stock") + row(688, "Bread", "2.50", "12") + row(676, "Milk", "1.20", "40"),
			[]string{"table:Item\tPrice\tStock\nBread\t2.50\t12\nMilk\t1.20\t40"}},
		{"rows, then a paragraph",
			row(700, "Bread", "2.50") + row(688, "Milk", "1.20") + draw(10, stack(72, 676, 12, paraA...)...),
			[]string{"Bread\t2.50\nMilk\t1.20", joined(paraA)}},
		{"a wide space inside a justified line is a space",
			// Word spacing of 9 points makes each gap 15 points: one and
			// a half times the font size.
			"BT /F1 10 Tf 9 Tw 1 0 0 1 72 700 Tm (wide gaps between these words) Tj ET",
			[]string{"wide gaps between these words"}},
		{"words set one by one, a little apart",
			"BT /F1 10 Tf 1 0 0 1 72 700 Tm (wide) Tj 40 0 Td (gaps) Tj 40 0 Td (here) Tj ET",
			[]string{"wide gaps here"}},
		{"a space character before a wide gap",
			"BT /F1 10 Tf 1 0 0 1 72 700 Tm (Name: ) Tj 150 0 Td ( Jane Doe) Tj ET",
			[]string{"Name:\tJane Doe"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := synthDoc{Font: fontCourier, Pages: []synthPage{{Content: tc.content}}}
			var got []string
			for _, b := range mustExtract(t, d.bytes(), nil).Parts[0].Blocks {
				if b.Kind != gofastcore.KindTable {
					got = append(got, b.Text)
					continue
				}
				var rows []string
				for _, r := range b.Table.Rows {
					rows = append(rows, strings.Join(r, "\t"))
				}
				got = append(got, "table:"+strings.Join(rows, "\n"))
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("blocks %q\nwant   %q", got, tc.want)
			}
		})
	}
}

// TestLetterSpacing: text set with its letters apart is read as words,
// judged against the other gaps of its line.
func TestLetterSpacing(t *testing.T) {
	// Each letter by itself, 9 points from the last: the gap of 3
	// points is the space threshold of this font exactly, so 10 it is.
	apart := func(x, y, step float64, s string) string {
		var sb strings.Builder
		fmt.Fprintf(&sb, "BT /F1 10 Tf 1 0 0 1 %g %g Tm ", x, y)
		for i, r := range s {
			if i > 0 {
				fmt.Fprintf(&sb, "%g 0 Td ", step)
			}
			fmt.Fprintf(&sb, "(%c) Tj ", r)
		}
		sb.WriteString("ET\n")
		return sb.String()
	}
	for _, tc := range []struct {
		name    string
		content string
		want    []string
	}{
		{"a heading with character spacing",
			"BT /F1 10 Tf 4 Tc 1 0 0 1 72 700 Tm (CONTENTS) Tj ET", []string{"CONTENTS"}},
		{"two words with character spacing and a space between",
			"BT /F1 10 Tf 4 Tc 1 0 0 1 72 700 Tm (ANNUAL REPORT) Tj ET", []string{"ANNUAL REPORT"}},
		{"letters placed one by one", apart(72, 700, 10, "CONTENTS"), []string{"CONTENTS"}},
		{"letters placed one by one, with a wider gap between two words",
			apart(72, 700, 10, "ANNUAL") + apart(72+6*10+12, 700, 10, "REPORT"), []string{"ANNUAL REPORT"}},
		{"a spaced heading over ordinary text",
			"BT /F1 10 Tf 4 Tc 1 0 0 1 72 720 Tm (SUMMARY) Tj 0 Tc ET\n" + draw(10, stack(72, 700, 12, paraA...)...),
			[]string{"SUMMARY", joined(paraA)}},
		{"a spaced sentence with its punctuation",
			"BT /F1 10 Tf 4 Tc 1 0 0 1 72 700 Tm (Agreed, in part.) Tj ET", []string{"Agreed, in part."}},
		// Not letters set apart:
		{"dots that lead to a page number",
			"BT /F1 10 Tf 1 0 0 1 72 700 Tm (Summary) Tj 54 0 Td (.) Tj " + strings.Repeat("12 0 Td (.) Tj ", 7) + "18 0 Td (12) Tj ET",
			[]string{"Summary . . . . . . . . 12"}},
		{"single letters among words, all gaps alike",
			"BT /F1 10 Tf 1 0 0 1 72 700 Tm [(points) -1000 (A) -1000 (B) -1000 (C) -1000 (D) -1000 (lie) -1000 (here)] TJ ET",
			[]string{"points A B C D lie here"}},
		{"single digits", apart(72, 700, 10, "12345"), []string{"1 2 3 4 5"}},
		{"three letters only", apart(72, 700, 10, "ABC"), []string{"A B C"}},
		{"letters far apart, as the heads of columns", apart(72, 700, 30, "ABCDE"), []string{"A\tB\tC\tD\tE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := blockTexts(t, tc.content); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("blocks %q\nwant   %q", got, tc.want)
			}
		})
	}
}

// TestTextPrintedTwice: the same text in the same place is one text,
// wherever its copies are in the page's content.
const (
	twoStrings   = "BT /F1 10 Tf 72 500 Td [(The quick ) -30 (brown fox)] TJ ET\n"
	threeStrings = "BT /F1 10 Tf 72 500 Td [(The quick ) -30 (brown fox ) 20 (jumps)] TJ ET\n"
)

func TestTextPrintedTwice(t *testing.T) {
	once := draw(10, stack(72, 700, 12, paraA...)...)
	shifted := draw(10, stack(72.4, 699.7, 12, paraA...)...)
	for _, tc := range []struct {
		name    string
		content string
		want    []string
	}{
		{"a paragraph printed twice, for bold", once + once, []string{joined(paraA)}},
		{"a paragraph and its shadow", once + shifted, []string{joined(paraA)}},
		{"three times, with other text between",
			once + draw(10, stack(72, 600, 12, paraB...)...) + once + shifted, []string{joined(paraA), joined(paraB)}},
		// One line, set as several strings as kerned text is, and printed
		// again at once: the copy begins before the end of the line.
		{"a line of two strings printed twice", twoStrings + twoStrings, []string{"The quick brown fox"}},
		{"a line of three strings printed twice", threeStrings + threeStrings, []string{"The quick brown fox jumps"}},
		{"a line of three strings and its shadow",
			threeStrings + strings.Replace(threeStrings, "72 500 Td", "72.4 499.7 Td", 1), []string{"The quick brown fox jumps"}},
		{"a line of two strings printed three times", twoStrings + twoStrings + twoStrings, []string{"The quick brown fox"}},
		{"other text in the same place is kept",
			once + draw(10, stack(72, 700, 12, paraB...)...), []string{joined(paraA), joined(paraB)}},
		{"the same text in another place is kept",
			once + draw(10, stack(72, 600, 12, paraA...)...), []string{joined(paraA), joined(paraA)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := blockTexts(t, tc.content)
			if strings.HasPrefix(tc.name, "other text in the same place") {
				// Both texts are there; how two paragraphs that lie on
				// top of each other are divided is not the point.
				all := strings.Join(got, " ")
				for _, w := range tc.want {
					for _, word := range strings.Fields(w) {
						if !strings.Contains(all, word) {
							t.Errorf("%q is missing from %q", word, got)
						}
					}
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("blocks %q\nwant   %q", got, tc.want)
			}
		})
	}
}

// TestLinesDrawnInPieces: a page that draws the words of a line at
// different times still has that line.
func TestLinesDrawnInPieces(t *testing.T) {
	bold := "/F2 << /Type /Font /Subtype /Type1 /BaseFont /Courier-Bold >>"
	// The regular text first, with holes; the bold words after it.
	// "The [committee] met on Tuesday to" and so on: a character is 6
	// points wide.
	regular := "BT /F1 10 Tf 1 0 0 1 72 700 Tm (The) Tj 1 0 0 1 156 700 Tm (met on Tuesday to) Tj\n" +
		"1 0 0 1 72 688 Tm (review the) Tj 1 0 0 1 180 688 Tm (for next year) Tj\n" +
		"1 0 0 1 72 676 Tm (and agreed on most of it.) Tj ET\n"
	bolds := "BT /F2 10 Tf 1 0 0 1 96 700 Tm (committee) Tj 1 0 0 1 138 688 Tm (budget) Tj ET\n"
	for name, content := range map[string]string{"bold words last": regular + bolds, "bold words first": bolds + regular} {
		d := synthDoc{Font: fontCourier, Pages: []synthPage{{Fonts: bold, Content: content}}}
		if got := texts(&mustExtract(t, d.bytes(), nil).Parts[0]); !reflect.DeepEqual(got, []string{joined(paraA)}) {
			t.Errorf("%s: blocks %q", name, got)
		}
	}

	t.Run("two columns with a narrow gutter are not one line", func(t *testing.T) {
		// The columns are 3 points apart, less than a space, and their
		// lines share baselines.
		left := stack(72, 700, 12, "The committee met on Tuesday to.", "review the budget for next year.", "and agreed on most of the items.")
		right := stack(72+32*6+3, 700, 12, "A second meeting will be held in", "the spring to settle the points.", "that remain open.")
		got := blockTexts(t, draw(10, cat(left, right)...))
		want := []string{"The committee met on Tuesday to. review the budget for next year. and agreed on most of the items.",
			"A second meeting will be held in the spring to settle the points. that remain open."}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("blocks %q", got)
		}
	})
	t.Run("cells of a row drawn at different times are apart", func(t *testing.T) {
		// Drawn column by column, the cells of each row touching.
		content := draw(10, textLine{72, 700, "Return"}, textLine{72, 688, "string"}) +
			draw(10, textLine{72 + 36, 700, "Name"}, textLine{72 + 36, 688, "title"})
		got := strings.Join(blockTexts(t, content), " ")
		for _, w := range []string{"Return", "Name", "string", "title"} {
			if !strings.Contains(" "+strings.NewReplacer("\n", " ", "\t", " ").Replace(got)+" ", " "+w+" ") {
				t.Errorf("%q is not a word of %q", w, got)
			}
		}
	})
}

// TestReadingOrder: the order of the blocks, whatever the order in
// which the page draws them.
func TestReadingOrder(t *testing.T) {
	title := []textLine{{72, 740, "Minutes of the meeting of the committee on the budget for next year"}}
	footer := []textLine{{280, 60, "Page 7"}}
	left := cat(stack(72, 700, 12, paraA...), stack(72, 650, 12, paraB...))
	right := cat(stack(320, 700, 12, paraC...), stack(320, 650, 12, paraA...))
	a, b, c := joined(paraA), joined(paraB), joined(paraC)
	want := []string{title[0].s, a, b, c, a, "Page 7"}

	// The same page, drawn in different orders.
	orders := map[string][]textLine{
		"as it is read":                    cat(title, left, right, footer),
		"the right column first":           cat(right, left, title, footer),
		"the footer first, the title last": cat(footer, left, right, title),
		"paragraph by paragraph across":    cat(title, left[:3], right[:3], left[3:], right[3:], footer),
	}
	for name, lines := range orders {
		t.Run("two columns/"+name, func(t *testing.T) {
			got := blockTexts(t, draw(10, lines...))
			if name == "paragraph by paragraph across" {
				// Drawn band by band, the page is read band by band:
				// the order of drawing decides where the geometry
				// allows both.
				want := []string{title[0].s, a, c, b, a, "Page 7"}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("blocks:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
				}
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("blocks:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
			}
		})
	}

	t.Run("three columns", func(t *testing.T) {
		cols := cat(stack(400, 700, 12, paraC...), stack(72, 700, 12, paraA...), stack(236, 700, 12, paraB...))
		if got := blockTexts(t, draw(8, cols...)); !reflect.DeepEqual(got, []string{a, b, c}) {
			t.Errorf("blocks %q", got)
		}
	})

	t.Run("a box beside the text", func(t *testing.T) {
		// A column of text and a narrow box to its right, drawn first.
		box := stack(420, 690, 12, "See also the", "report of 2023.")
		body := cat(stack(72, 700, 12, paraA...), stack(72, 650, 12, paraB...))
		got := blockTexts(t, draw(10, cat(box, body)...))
		if !reflect.DeepEqual(got, []string{a, b, "See also the report of 2023."}) {
			t.Errorf("blocks %q", got)
		}
	})

	t.Run("cells of several lines", func(t *testing.T) {
		cell := func(x, y float64, name string) []textLine {
			return stack(x, y, 12, name+" first line of the cell", name+" second line of it.")
		}
		rows := cat(cell(72, 700, "A1"), cell(320, 700, "B1"), cell(72, 660, "A2"), cell(320, 660, "B2"))
		byRows := []string{"A1 first line of the cell A1 second line of it.", "B1 first line of the cell B1 second line of it.",
			"A2 first line of the cell A2 second line of it.", "B2 first line of the cell B2 second line of it."}
		// Drawn row by row, a table is read by rows.
		if got := blockTexts(t, draw(10, rows...)); !reflect.DeepEqual(got, byRows) {
			t.Errorf("drawn by rows: %q", got)
		}
		// Drawn column by column, the same boxes are two columns.
		cols := cat(cell(72, 700, "A1"), cell(72, 660, "A2"), cell(320, 700, "B1"), cell(320, 660, "B2"))
		byCols := []string{byRows[0], byRows[2], byRows[1], byRows[3]}
		if got := blockTexts(t, draw(10, cols...)); !reflect.DeepEqual(got, byCols) {
			t.Errorf("drawn by columns: %q", got)
		}
	})

	t.Run("text in other directions", func(t *testing.T) {
		// A paragraph, and a note written up the margin.
		content := draw(10, stack(72, 700, 12, paraA...)...) +
			"BT /F1 10 Tf 0 1 -1 0 40 300 Tm (Printed on recycled paper) Tj ET"
		got := blockTexts(t, content)
		if !reflect.DeepEqual(got, []string{a, "Printed on recycled paper"}) && !reflect.DeepEqual(got, []string{"Printed on recycled paper", a}) {
			t.Errorf("blocks %q", got)
		}
	})

	t.Run("a page turned on its side", func(t *testing.T) {
		// The same two columns on a page whose content is rotated and
		// which says so: what is displayed is the same page.
		d := synthDoc{Font: fontCourier, Pages: []synthPage{{
			Content:  "q 0 1 -1 0 792 0 cm " + draw(10, cat(title, right, left, footer)...) + " Q",
			MediaBox: "[0 0 792 612]", Extra: "/Rotate 90"}}}
		if got := texts(&mustExtract(t, d.bytes(), nil).Parts[0]); !reflect.DeepEqual(got, want) {
			t.Errorf("blocks:\n  %s", strings.Join(got, "\n  "))
		}
	})
}
