package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
)

func itemsOf(doc *gofastcore.Document) (out []string) {
	for _, p := range doc.Parts {
		for _, b := range p.Blocks {
			switch b.Kind {
			case gofastcore.KindListItem:
				out = append(out, fmt.Sprintf("L%d %v %q %q", b.Level, b.Ordered, b.Marker, b.Text))
			case gofastcore.KindHeading:
				out = append(out, fmt.Sprintf("H%d %q %q", b.Level, b.Marker, b.Text))
			default:
				out = append(out, fmt.Sprintf("%s %q", b.Kind, b.Text))
			}
		}
	}
	return out
}

// evenBody is a paragraph of three lines, the first two of one length.
func evenBody(y int) string {
	return set("F1", 10, 72, y, "The body of the section goes on for a line") +
		set("F1", 10, 72, y-12, "and then for another line of the same size") +
		set("F1", 10, 72, y-24, "before it ends.")
}

func TestListItems(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    []string
	}{
		{"bullets, one nested, set one under the other",
			para(720, "Before the list") +
				set("F1", 10, 72, 670, "\\267 Bread") +
				set("F1", 10, 72, 658, "\\267 Two bottles of milk") +
				set("F1", 10, 90, 646, "\\267 Skimmed") +
				set("F1", 10, 72, 634, "\\267 A newspaper for the weekend"),
			[]string{
				`paragraph "Before the list and some more words of the body that go on for a second line of the same paragraph and end on a third line here."`,
				`L1 false "•" "Bread"`, `L1 false "•" "Two bottles of milk"`, `L2 false "•" "Skimmed"`, `L1 false "•" "A newspaper for the weekend"`,
			}},
		{"numbered items whose lines wrap under their text",
			set("F1", 10, 72, 700, "1. The committee met on Tuesday to review") +
				set("F1", 10, 90, 688, "the budget for next year.") +
				set("F1", 10, 72, 676, "2. A second meeting will be held in the") +
				set("F1", 10, 90, 664, "spring to settle the rest.") +
				// A bracket in a string of the file is escaped.
				set("F1", 10, 90, 652, "a\\) Lettered under the second") +
				set("F1", 10, 72, 640, "(3) The third in brackets"),
			[]string{
				`L1 true "1." "The committee met on Tuesday to review the budget for next year."`,
				`L1 true "2." "A second meeting will be held in the spring to settle the rest."`,
				`L2 true "a)" "Lettered under the second"`,
				`L1 true "(3)" "The third in brackets"`,
			}},
		{"not items: a decimal number, a year, an initial, a dash in prose",
			set("F1", 10, 72, 700, "3.5 million people live there.") +
				set("F1", 10, 72, 660, "2001. The year it all began.") +
				set("F1", 10, 72, 620, "J. Smith said so.") +
				set("F1", 10, 72, 580, "The answer was clear to all of them at once") +
				set("F1", 10, 72, 568, "\\320 which nobody had expected."),
			[]string{
				`paragraph "3.5 million people live there."`,
				`paragraph "2001. The year it all began."`,
				`paragraph "J. Smith said so."`,
				`paragraph "The answer was clear to all of them at once — which nobody had expected."`,
			}},
		{"a numbered heading keeps its number",
			set("F1", 16, 72, 720, "2. Methods") + evenBody(690) + evenBody(640),
			[]string{
				`H2 "2." "Methods"`,
				`paragraph "The body of the section goes on for a line and then for another line of the same size before it ends."`,
				`paragraph "The body of the section goes on for a line and then for another line of the same size before it ends."`,
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: tc.content}}}.bytes(), nil)
			if got := itemsOf(doc); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}

	// The renderers put the marker back in front.
	content := set("F1", 10, 72, 700, "\\267 Bread") + set("F1", 10, 72, 688, "\\267 Milk")
	doc := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: content}}}.bytes(), nil)
	if md := doc.Markdown(); !strings.Contains(md, "Bread") || !strings.Contains(md, "Milk") || strings.Count(md, "•")+strings.Count(md, "- ") < 2 {
		t.Errorf("markdown %q", md)
	}
}
