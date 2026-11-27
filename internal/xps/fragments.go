package xps

import (
	"strings"

	"github.com/AOShei/go-fast-core/opc"
	"github.com/AOShei/go-fast-core/xmlscan"
)

// Story fragments: the structure a page declares.
//
// A page can have a StoryFragments part, which says of its named
// elements what they are: the paragraphs, the lists and their items, the
// tables with their rows and cells, in the order they are read, and
// whether a fragment is the header or the footer of its page. It is what
// the structure tree is to a tagged PDF, and is used as that is.

// The kinds of a fragment.
const (
	FragmentContent = iota
	FragmentHeader
	FragmentFooter
)

// Bounds on the structure of one page.
const (
	maxMarks       = 1 << 17 // named elements
	maxCells       = 1 << 16 // cells of the tables of a page
	maxSpan        = 1 << 10 // the rows or columns one cell spans
	maxFragDepth   = 64      // nesting of structure elements
	relFragmentsXP = nsXPS + "/storyfragments"
	relFragmentsOX = nsOpenXPS + "/storyfragments"
	typeFragments  = "application/vnd.ms-package.xps-storyfragments+xml"
)

const (
	spRels = xmlscan.FirstSpace + 32 + iota
)

var relsOptions = xmlscan.Options{Spaces: map[string]xmlscan.Space{
	"http://schemas.openxmlformats.org/package/2006/relationships": spRels,
}}

// Marks is what the fragments of a page say of its named elements.
type Marks struct {
	// Names gives the mark of a named element: an index into Marks.
	Names map[string]int32
	Marks []Mark
	// Cells are the cells of the page's tables, table by table, and in a
	// table row by row.
	Cells []Cell
	// Tables are the grids of the tables.
	Tables []Grid
}

// Mark is what is declared of one named element.
type Mark struct {
	// Rank is the place of the element in the order of the fragments,
	// from 1.
	Rank int32
	// Para is the paragraph it is in, from 1; 0 for none.
	Para int32
	// Item is the list item it is in, from 1, and Label is set when it
	// is the marker of that item; 0 for none.
	Item  int64
	Label bool
	// Cell is the cell of a table it is in: an index into Marks.Cells,
	// -1 for none.
	Cell int32
	// Kind is the kind of the fragment it is in.
	Kind uint8
}

// Cell is a cell of a table: where it is in the grid of its table.
type Cell struct {
	Table                int32
	Row, Col, Rows, Cols int32
}

// Grid is the size of a table.
type Grid struct{ Rows, Cols int32 }

// marks reads the story fragments of the page that is the part name;
// nil when it has none, or when they name nothing.
func (d *Document) marks(name string, s *xmlscan.Scanner, info *PageInfo) (m *Marks, damaged bool) {
	if d.pkg.Typed() && !d.pkg.HasContentType(typeFragments) {
		return nil, false // no part of the package is one: most have none
	}
	// The relationships of the page say which part it is. They are read
	// here and not by the package, so that they are counted to the page.
	part := ""
	if rels := opc.RelsName(name); d.pkg.Has(rels) {
		closer, err := d.scan(rels, s, &relsOptions, info)
		if err != nil {
			return nil, false
		}
		for part == "" {
			k := s.Next()
			if k == xmlscan.EOF {
				break
			}
			if k != xmlscan.Start || !s.Is(spRels, "Relationship") {
				continue
			}
			typ, _ := s.Attr(xmlscan.NoSpace, "Type")
			if string(typ) != relFragmentsXP && string(typ) != relFragmentsOX {
				continue
			}
			if mode, ok := s.Attr(xmlscan.NoSpace, "TargetMode"); ok && string(mode) == "External" {
				continue
			}
			if target, ok := s.Attr(xmlscan.NoSpace, "Target"); ok {
				if p := opc.Resolve(name, string(target)); d.pkg.Has(p) {
					part = p
				}
			}
		}
		closer.Close()
	}
	if part == "" {
		return nil, false
	}
	closer, err := d.scan(part, s, &structureOptions, info)
	if err != nil {
		return nil, true
	}
	defer closer.Close()
	f := fragments{s: s, m: &Marks{Names: map[string]int32{}}}
	for {
		k := s.Next()
		if k == xmlscan.EOF {
			break
		}
		if k != xmlscan.Start || !s.Is(spStructure, "StoryFragment") {
			continue
		}
		kind := uint8(FragmentContent)
		if v, ok := s.Attr(xmlscan.NoSpace, "FragmentType"); ok {
			switch string(v) {
			case "Header":
				kind = FragmentHeader
			case "Footer":
				kind = FragmentFooter
			}
		}
		f.kind = kind
		f.children(scope{cell: -1}, 0)
	}
	damaged = s.Err() != nil
	if len(f.m.Marks) == 0 {
		return nil, damaged
	}
	f.grids()
	return f.m, damaged
}

// fragments reads a StoryFragments part.
type fragments struct {
	s    *xmlscan.Scanner
	m    *Marks
	kind uint8
	// What has been numbered so far.
	paras  int32
	items  int64
	tables int32
	// rows are the rows of the tables: of each cell, the row it is in,
	// by the order of the rows of its table.
	cellRow []int32
}

// scope is what an element is in.
type scope struct {
	para   int32
	item   int64
	marker string // the name of the element that is the marker of the item
	cell   int32
	table  int32 // one more than the table that is open, 0 for none
	row    int32 // the row that is open, from 0; -1 before the first
}

// children reads the elements in the structure element whose start tag
// is current, to its end.
func (f *fragments) children(in scope, depth int) {
	s := f.s
	rows := int32(-1) // the rows of the table read so far, for a table
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			return
		}
		if k != xmlscan.Start {
			continue
		}
		if s.Space() != spStructure || depth >= maxFragDepth {
			s.Skip()
			continue
		}
		sc := in
		switch string(s.Local()) {
		case "NamedElement":
			v, ok := s.Attr(xmlscan.NoSpace, "NameReference")
			s.Skip()
			if !ok || len(v) == 0 || len(v) > MaxName || len(f.m.Marks) >= maxMarks {
				continue
			}
			name := string(v)
			if _, seen := f.m.Names[name]; seen {
				continue // the first place of an element is its place
			}
			f.m.Names[name] = int32(len(f.m.Marks))
			f.m.Marks = append(f.m.Marks, Mark{Rank: int32(len(f.m.Marks)) + 1, Para: in.para, Item: in.item,
				Label: in.item != 0 && in.marker == name, Cell: in.cell, Kind: f.kind})
			continue
		case "ParagraphStructure":
			f.paras++
			sc.para = f.paras
		case "ListItemStructure":
			f.items++
			sc.item, sc.marker = f.items, ""
			if v, ok := s.Attr(xmlscan.NoSpace, "Marker"); ok && len(v) <= MaxName {
				sc.marker = strings.TrimSpace(string(v))
			}
		case "TableStructure":
			if in.table != 0 || in.cell >= 0 {
				// A table in a cell: its text is the cell's.
				f.children(in, depth+1)
				continue
			}
			f.tables++
			sc.table, sc.row = f.tables, -1
			f.m.Tables = append(f.m.Tables, Grid{})
		case "TableRowGroupStructure":
			if in.table == 0 || in.cell >= 0 {
				f.children(in, depth+1)
				continue
			}
			sc.row = rows
			f.rowGroup(sc, depth+1, &rows)
			continue
		case "TableRowStructure":
			if in.table == 0 || in.cell >= 0 {
				f.children(in, depth+1)
				continue
			}
			rows++
			sc.row = rows
		case "TableCellStructure":
			if in.table == 0 || in.cell >= 0 || in.row < 0 || len(f.m.Cells) >= maxCells {
				f.children(in, depth+1)
				continue
			}
			c := Cell{Table: in.table - 1, Rows: 1, Cols: 1}
			if v, ok := s.Attr(xmlscan.NoSpace, "RowSpan"); ok {
				if n, good := parseNumber(v); good && n >= 1 {
					c.Rows = int32(min(n, maxSpan))
				}
			}
			if v, ok := s.Attr(xmlscan.NoSpace, "ColumnSpan"); ok {
				if n, good := parseNumber(v); good && n >= 1 {
					c.Cols = int32(min(n, maxSpan))
				}
			}
			sc.cell = int32(len(f.m.Cells))
			f.m.Cells = append(f.m.Cells, c)
			f.cellRow = append(f.cellRow, in.row)
		}
		// Sections, figures, lists and what else there is: what is in
		// them is where they are.
		f.children(sc, depth+1)
	}
}

// rowGroup reads a group of rows: its rows are rows of the table.
func (f *fragments) rowGroup(in scope, depth int, rows *int32) {
	s := f.s
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			return
		}
		if k != xmlscan.Start {
			continue
		}
		if !s.Is(spStructure, "TableRowStructure") || depth >= maxFragDepth {
			s.Skip()
			continue
		}
		*rows++
		sc := in
		sc.row = *rows
		f.children(sc, depth+1)
	}
}

// grids places the cells of each table in its grid, as an HTML table is
// laid out: each cell takes the first column of its row that no cell
// above it still covers.
func (f *fragments) grids() {
	m := f.m
	for t := range m.Tables {
		// The cells of the table are together, row by row.
		var covered []int32 // by column: the row up to which it is covered
		row := int32(-1)
		col := int32(0)
		for i := range m.Cells {
			c := &m.Cells[i]
			if int(c.Table) != t {
				continue
			}
			if r := f.cellRow[i]; r != row {
				row, col = r, 0
			}
			for int(col) < len(covered) && covered[col] >= row {
				col++
			}
			c.Row, c.Col = row, col
			for k := col; k < col+c.Cols; k++ {
				for int(k) >= len(covered) {
					covered = append(covered, -1)
				}
				covered[k] = row + c.Rows - 1
			}
			col += c.Cols
			m.Tables[t].Rows = max(m.Tables[t].Rows, row+1)
			m.Tables[t].Cols = max(m.Tables[t].Cols, col)
		}
	}
}
