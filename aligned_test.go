package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
)

// alignedDoc is a page of Courier, with Courier-Bold as /F2, whose
// content is rows: each row a line of cells at the x positions given,
// 14 points apart from y down, in bold when the row says so.
func alignedDoc(y int, xs []int, rows []struct {
	bold  bool
	cells []string
}, before, after string) synthDoc {
	var sb strings.Builder
	sb.WriteString(before)
	for i, r := range rows {
		font := "F1"
		if r.bold {
			font = "F2"
		}
		for k, c := range r.cells {
			if c != "" {
				sb.WriteString(set(font, 10, xs[k], y-14*i, c))
			}
		}
	}
	sb.WriteString(after)
	return synthDoc{Font: fontCourier, Pages: []synthPage{{Content: sb.String(), Fonts: boldFont}}}
}

type row = struct {
	bold  bool
	cells []string
}

// tableBlocks returns the blocks of a document's first page as
// tablesOf describes them.
func tableBlocks(t *testing.T, d synthDoc) string {
	t.Helper()
	doc := mustExtract(t, d.bytes(), nil)
	return tablesOf(doc.Parts[0])
}

func TestAlignedTable(t *testing.T) {
	rows := []row{
		{true, []string{"Station", "Depth", "Count"}},
		{false, []string{"North Bay", "12.5", "140"}},
		{false, []string{"South Cove", "8.0", "97"}},
		{false, []string{"East Reef", "15.2", "211"}},
		{false, []string{"West Point", "4.4", "36"}},
	}
	before := para(740, "Text before the table")
	after := para(560, "Text after the table")
	got := tableBlocks(t, alignedDoc(680, []int{72, 200, 300}, rows, before, after))
	want := `paragraph "Text before the table and some more words of the body that go on for a second line of the same paragraph and end on a third line here."
table inferred continued=false header=1 merges=[]
  | Station | Depth | Count |
  | North Bay | 12.5 | 140 |
  | South Cove | 8.0 | 97 |
  | East Reef | 15.2 | 211 |
  | West Point | 4.4 | 36 |
paragraph "Text after the table and some more words of the body that go on for a second line of the same paragraph and end on a third line here."
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	t.Run("columns a space and a half apart", func(t *testing.T) {
		// Narrower than any space a paragraph would leave, wider than one
		// between words; lined up down the rows.
		rows := []row{
			{false, []string{"1939", "1", "647295", "643462"}},
			{false, []string{"1939", "2", "651280", "650013"}},
			{false, []string{"1939", "3", "652650", "681379"}},
			{false, []string{"1939", "4", "585628", "618892"}},
		}
		got := tableBlocks(t, alignedDoc(700, []int{72, 111, 132, 183}, rows, "", ""))
		if !strings.HasPrefix(got, "table inferred") || !strings.Contains(got, "| 1939 | 2 | 651280 | 650013 |") {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("a cell of two lines", func(t *testing.T) {
		rows := []row{
			{false, []string{"Item", "Use", "Cost"}},
			{false, []string{"Pump", "Moves water", "120"}},
			{false, []string{"", "uphill", ""}},
			{false, []string{"Valve", "Stops flow", "40"}},
			{false, []string{"Gauge", "Reads level", "25"}},
		}
		// The second line of a cell is set closer than the rows are.
		d := alignedDoc(700, []int{72, 200, 330}, rows, "", "")
		d.Pages[0].Content = strings.Replace(d.Pages[0].Content, "200 672 Td (uphill)", "200 676 Td (uphill)", 1)
		got := tableBlocks(t, d)
		if !strings.Contains(got, "| Pump | Moves water uphill | 120 |") || strings.Count(got, "\n  |") != 4 {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("rows with the first cell empty, as far apart as the others", func(t *testing.T) {
		// All rows as far apart: a row with its first cell empty is a
		// row, not more lines of the cells of the row over it.
		rows := []row{
			{false, []string{"Korn", "19:15", "Main stage"}},
			{false, []string{"Staind", "14:40", "Main stage"}},
			{false, []string{"Bane", "13:00", "Tent"}},
			{false, []string{"", "15:05", "Tent"}},
			{false, []string{"", "13:45", "Second stage"}},
		}
		got := tableBlocks(t, alignedDoc(700, []int{72, 200, 300}, rows, "", ""))
		if !strings.Contains(got, "|  | 15:05 | Tent |") || !strings.Contains(got, "|  | 13:45 | Second stage |") {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("tables side by side", func(t *testing.T) {
		// A name and a time for each of two stages, one list longer:
		// two tables, each read down.
		rows := []row{
			{false, []string{"Faith No More", "21:00 - 22:45", "Motley Crue", "20:35 - 21:45"}},
			{false, []string{"Korn", "19:15 - 20:30", "Opeth", "19:10 - 20:00"}},
			{false, []string{"Limp Bizkit", "17:45 - 18:45", "Lacuna Coil", "18:00 - 18:45"}},
			{false, []string{"", "", "Voivod", "16:20 - 16:50"}},
		}
		got := tableBlocks(t, alignedDoc(700, []int{72, 200, 330, 450}, rows, "", ""))
		want := `table inferred continued=false header=0 merges=[]
  | Faith No More | 21:00 - 22:45 |
  | Korn | 19:15 - 20:30 |
  | Limp Bizkit | 17:45 - 18:45 |
table inferred continued=false header=0 merges=[]
  | Motley Crue | 20:35 - 21:45 |
  | Opeth | 19:10 - 20:00 |
  | Lacuna Coil | 18:00 - 18:45 |
  | Voivod | 16:20 - 16:50 |
`
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("columns of words and numbers in turn are one table", func(t *testing.T) {
		// The kinds of the columns repeat, but every row is full: one
		// table, not two.
		rows := []row{
			{false, []string{"Ann Baker", "34", "Leeds", "2041"}},
			{false, []string{"Carl Dunn", "51", "York", "1187"}},
			{false, []string{"Eve Fox", "27", "Bath", "3302"}},
			{false, []string{"Gus Hart", "45", "Hull", "2954"}},
		}
		got := tableBlocks(t, alignedDoc(700, []int{72, 200, 260, 380}, rows, "", ""))
		if strings.Count(got, "table inferred") != 1 || !strings.Contains(got, "| Ann Baker | 34 | Leeds | 2041 |") {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("bold throughout is no header", func(t *testing.T) {
		rows := []row{
			{true, []string{"Alpha", "1", "one"}},
			{true, []string{"Beta", "2", "two"}},
			{true, []string{"Gamma", "3", "three"}},
			{true, []string{"Delta", "4", "four"}},
		}
		got := tableBlocks(t, alignedDoc(700, []int{72, 200, 300}, rows, "", ""))
		if !strings.HasPrefix(got, "table inferred continued=false header=0") {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("a label of a group in bold is no header", func(t *testing.T) {
		rows := []row{
			{true, []string{"Coast", "", ""}},
			{false, []string{"North Bay", "12.5", "140"}},
			{false, []string{"South Cove", "8.0", "97"}},
			{false, []string{"East Reef", "15.2", "211"}},
		}
		got := tableBlocks(t, alignedDoc(700, []int{72, 200, 300}, rows, "", ""))
		if !strings.Contains(got, "header=0") {
			t.Errorf("got:\n%s", got)
		}
	})
}

// Text set in columns that is not a table stays paragraphs.
func TestAlignedNotTables(t *testing.T) {
	cases := map[string]struct {
		xs   []int
		rows []row
	}{
		"two columns of prose": {[]int{72, 320}, []row{
			{false, []string{"The committee met on the", "Members asked about the"}},
			{false, []string{"first day of the month to", "costs of the new building"}},
			{false, []string{"discuss the budget for the", "and the schedule for its"}},
			{false, []string{"coming year and its plans.", "completion by the spring."}},
		}},
		"a list with bullets": {[]int{72, 90}, []row{
			{false, []string{"\267", "Jackson County"}},
			{false, []string{"\267", "Harrison County"}},
			{false, []string{"\267", "Hancock County"}},
			{false, []string{"\267", "Stone County"}},
		}},
		"a numbered list": {[]int{72, 100}, []row{
			{false, []string{"1.", "Open the valve"}},
			{false, []string{"2.", "Fill the tank"}},
			{false, []string{"3.", "Close the valve"}},
			{false, []string{"4.", "Read the gauge"}},
		}},
		"a table of contents": {[]int{72, 400}, []row{
			{false, []string{"Introduction . . . . . . . .", "1"}},
			{false, []string{"Methods . . . . . . . . . . .", "4"}},
			{false, []string{"Results . . . . . . . . . . .", "9"}},
			{false, []string{"Discussion . . . . . . . . .", "15"}},
		}},
		"names set in columns": {[]int{72, 220, 368}, []row{
			{false, []string{"Ann Baker", "Carl Dunn", "Eve Fox"}},
			{false, []string{"Gus Hart", "Ida Jones", "Kim Lee"}},
			{false, []string{"Max Neal", "Ora Pike", "Quin Ross"}},
			{false, []string{"Sam Tate", "Uma Vale", "Will York"}},
		}},
		"code with its comments": {[]int{72, 250}, []row{
			{false, []string{"mov eax, 1", "// count"}},
			{false, []string{"add eax, ebx", "// add the step"}},
			{false, []string{"cmp eax, 10", "// at the end?"}},
			{false, []string{"jne loop", "// go round"}},
		}},
		"a time scale repeated for each stage": {[]int{72, 200, 330}, []row{
			{false, []string{"11:25", "11:25", "11:25"}},
			{false, []string{"11:30", "11:30", "11:30"}},
			{false, []string{"11:35", "11:35", "11:35"}},
			{false, []string{"11:40", "11:40", "11:40 Trigger"}},
		}},
		"two rows": {[]int{72, 200, 300}, []row{
			{false, []string{"North Bay", "12.5", "140"}},
			{false, []string{"South Cove", "8.0", "97"}},
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := tableBlocks(t, alignedDoc(700, c.xs, c.rows, "", ""))
			if strings.Contains(got, "table") {
				t.Errorf("a table:\n%s", got)
			}
		})
	}
}

// The lines of a table found by alignment are in Page.Lines.
func TestAlignedTableLines(t *testing.T) {
	rows := []row{
		{false, []string{"North Bay", "12.5", "140"}},
		{false, []string{"South Cove", "8.0", "97"}},
		{false, []string{"East Reef", "15.2", "211"}},
	}
	d := alignedDoc(700, []int{72, 200, 300}, rows, "", "")
	lines, part := linesOf(t, d.bytes(), &gofastcore.Options{}, 1)
	if len(part.Blocks) != 1 || part.Blocks[0].Table == nil {
		t.Fatalf("%s", tablesOf(part))
	}
	var texts []string
	for _, l := range lines {
		texts = append(texts, fmt.Sprintf("%d %s", l.Block, l.Text))
	}
	if got := strings.Join(texts, "|"); !strings.Contains(got, "North Bay") || !strings.Contains(got, "211") {
		t.Errorf("lines %q", texts)
	}
}

// Text with a display reason is never a table's: a table is found among
// the text without one, and what has a reason stays a block of its own
// with it, as in a tagged table (tables_test.go).
func TestAlignedTableDisplay(t *testing.T) {
	rows := []row{
		{false, []string{"North Bay", "12.5", "140"}},
		{false, []string{"South Cove", "8.0", "97"}},
		{false, []string{"East Reef", "15.2", ""}},
		{false, []string{"West Point", "4.4", "36"}},
	}
	t.Run("a cell drawn with no ink", func(t *testing.T) {
		// The third row's last cell is in render mode 3.
		d := alignedDoc(680, []int{72, 200, 300}, rows, "", "BT 3 Tr /F1 10 Tf 300 652 Td (211) Tj 0 Tr ET\n")
		want := `table inferred continued=false header=0 merges=[]
  | North Bay | 12.5 | 140 |
  | South Cove | 8.0 | 97 |
  | East Reef | 15.2 |  |
  | West Point | 4.4 | 36 |
paragraph no_ink "211"
`
		if got := tableBlocks(t, d); got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("a table in a layer that is off", func(t *testing.T) {
		full := append([]row(nil), rows...)
		full[2] = row{false, []string{"East Reef", "15.2", "211"}}
		d := alignedDoc(680, []int{72, 200, 300}, full, "/OC /Layer BDC\n", "EMC\n")
		d.Objects = []string{"<< /Type /OCG /Name (Off) >>"}
		d.Catalog = "/OCProperties << /OCGs [1 0 R] /D << /OFF [1 0 R] >> >>"
		d.Pages[0].Resources = "/Properties << /Layer 1 0 R >>"
		doc := mustExtract(t, d.bytes(), nil)
		var text []string
		for _, b := range doc.Parts[0].Blocks {
			if b.Table != nil || b.Display != gofastcore.DisplayHidden {
				t.Errorf("a %s block with display %q: %q", b.Kind, b.Display, b.Text)
			}
			text = append(text, b.Text)
		}
		if got := strings.Join(text, " "); !strings.Contains(got, "North Bay") || !strings.Contains(got, "211") {
			t.Errorf("the text of the layer: %q", got)
		}
	})
}
