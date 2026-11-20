package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
)

const styleFonts = "/F2 << /Type /Font /Subtype /Type1 /BaseFont /Courier-Bold >>" +
	" /F3 << /Type /Font /Subtype /Type1 /BaseFont /Courier-Oblique >>"

// spansOf writes the spans of the blocks as the text they cover.
func spansOf(doc *gofastcore.Document) (out []string) {
	for _, p := range doc.Parts {
		for _, b := range p.Blocks {
			for _, s := range b.Spans {
				what := s.Style.String()
				if s.Href != "" {
					what = "link " + s.Href
				}
				out = append(out, fmt.Sprintf("%s %q", what, b.Text[s.Start:s.End]))
			}
		}
	}
	return out
}

func TestSpans(t *testing.T) {
	content := "BT /F1 10 Tf 72 700 Td (Plain words, ) Tj /F2 10 Tf (bold words) Tj /F1 10 Tf ( and ) Tj /F3 10 Tf (slanted) Tj /F1 10 Tf ( ones.) Tj ET\n" +
		set("F1", 10, 72, 660, "See the manual for more.") +
		set("F1", 10, 72, 620, "Back to the start of it.") +
		"BT /F1 10 Tf 72 580 Td (\\267 ) Tj /F2 10 Tf (A bold item) Tj ET\n"
	// "manual" is at 120 to 156 on the line at 660, "start" at 144 to 174
	// on the line at 620: the page is 792 high, so displayed they are at
	// 132 and 172 from the top.
	annots := "/Annots [" +
		"<< /Type /Annot /Subtype /Link /Rect [118 655 158 670] /A << /S /URI /URI (https://example.com/manual) >> >>" +
		"<< /Type /Annot /Subtype /Link /Rect [142 615 176 630] /Dest [1 /Fit] >>" +
		"<< /Type /Annot /Subtype /Link /F 2 /Rect [70 575 200 590] /A << /S /URI /URI (https://example.com/hidden) >> >>" +
		"]"
	d := synthDoc{Font: fontCourier, Pages: []synthPage{{Content: content, Fonts: styleFonts, Extra: annots}, {Content: set("F1", 10, 72, 700, "The second page.")}}}

	doc := mustExtract(t, d.bytes(), &gofastcore.Options{Spans: true})
	want := []string{
		`bold "bold words"`,
		`italic "slanted"`,
		`link https://example.com/manual "manual"`,
		`link #page=2 "start"`,
		`bold "A bold item"`,
	}
	if got := spansOf(doc); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("spans:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if md := doc.Markdown(); !strings.Contains(md, "[manual](<https://example.com/manual>)") && !strings.Contains(md, "[manual](https://example.com/manual)") {
		t.Errorf("markdown %q", md)
	}

	// Without the option, none.
	if got := spansOf(mustExtract(t, d.bytes(), nil)); len(got) != 0 {
		t.Errorf("spans without the option: %q", got)
	}

	// Link targets count against the limit on text.
	long := "https://example.com/" + strings.Repeat("a", 3000)
	d.Pages[0].Extra = "/Annots [<< /Type /Annot /Subtype /Link /Rect [118 655 158 670] /A << /S /URI /URI (" + long + ") >> >>]"
	opts := &gofastcore.Options{Spans: true}
	opts.Limits.MaxTextBytes = 3000
	doc = mustExtract(t, d.bytes(), opts)
	if !strings.Contains(fmt.Sprint(doc.Parts[0].Warnings), string(gofastcore.WarnLimitExceeded)) {
		t.Errorf("a link of 3,000 bytes in a limit of 3,000: warnings %v", doc.Parts[0].Warnings)
	}
}
