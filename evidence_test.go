package gofastpdf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
)

// TestEvidence counts, over the public corpus, what the plan asks to be
// measured before thresholds and warnings are settled: how often each
// hidden reason is found, how many pages the default contrast check
// cannot judge, and the two facts of proposal E. It writes a report and
// is not a test of anything.
//
//	GOFASTPDF_EVIDENCE=<directory> go test -run TestEvidence .
//
// With GOFASTPDF_EVIDENCE_DEEP=1 the pages are read with DeepHidden.
//
// The report gives the counts for the tuning set and the held-out set.
// Samples of flagged text are written for the tuning set only: the
// held-out files are not to be looked at one by one.
func TestEvidence(t *testing.T) {
	dir := os.Getenv("GOFASTPDF_EVIDENCE")
	if dir == "" {
		t.Skip("GOFASTPDF_EVIDENCE is not set")
	}
	cache := os.Getenv("GOFASTPDF_CACHE")
	if cache == "" {
		home, _ := os.UserHomeDir()
		cache = filepath.Join(home, ".cache", "gofastpdf-corpus")
	}
	var manifest []struct {
		Name string `json:"name"`
		Set  string `json:"set"`
	}
	data, err := os.ReadFile("testdata/corpus-public.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}

	type counts struct {
		files, read, pages, textPages int
		// By hidden reason: pages and blocks, and files.
		reasonPages, reasonBlocks, reasonFiles map[string]int
		hiddenInfo, hiddenWarning              int // pages by the severity of text.hidden
		unjudgedPages                          int // text whose contrast is not judged
		unjudgedRuns, judgedRuns               int
		incompletePages                        int // pdf.deep_hidden_incomplete
		mixedLayerPages                        int // a text layer and visible text on one page
		imageOnly, noText, failed              int
		actualDiffers, actualLonger            int // sequences
		actualDiffersPages, actualLongerPages  int
		longGlyphPages                         int
		tabPages, tabs                         int
		spacedPages, spacedLines               int
		duplicatePages, duplicates             int
		rejoinedPages                          int
		reorderedPages                         int
		rtlPages                               int
		active, external, repaired             int // files
		rebuilt, redefined                     int // files
	}
	newCounts := func() *counts {
		return &counts{reasonPages: map[string]int{}, reasonBlocks: map[string]int{}, reasonFiles: map[string]int{}}
	}
	sets := map[string]*counts{"tune": newCounts(), "held": newCounts()}
	samples := map[string][]string{}

	e := new(Extractor)
	opts := &gofastcore.Options{DeepHidden: os.Getenv("GOFASTPDF_EVIDENCE_DEEP") != ""}
	perReason := 40
	if opts.DeepHidden {
		perReason = 400
	}
	for _, m := range manifest {
		c := sets[m.Set]
		if c == nil {
			continue
		}
		c.files++
		data, err := os.ReadFile(filepath.Join(cache, m.Name))
		if err != nil {
			continue
		}
		f, err := file.OpenPassword(data, file.Limits{}, "")
		if err != nil {
			continue
		}
		c.read++
		fileReasons := map[string]bool{}
		active, external := false, false
		for i := range f.Pages() {
			out := e.page(f, &f.Pages()[i], opts)
			part := &out.part
			c.pages++
			active, external = active || out.active, external || out.external
			text, visible, layer := false, false, false
			pageReasons := map[string]bool{}
			for j := range part.Blocks {
				b := &part.Blocks[j]
				if b.Kind != gofastcore.KindParagraph {
					continue
				}
				text = true
				if b.Hidden == 0 {
					visible = true
					continue
				}
				layer = layer || b.Hidden&gofastcore.HiddenTextLayer != 0
				for _, name := range strings.Split(b.Hidden.String(), "+") {
					c.reasonBlocks[name]++
					pageReasons[name] = true
					if m.Set == "tune" && len(samples[name]) < perReason {
						r := b.Loc.BBox
						samples[name] = append(samples[name], fmt.Sprintf("%s page %d [%s] (%.0f %.0f %.0f %.0f of %.0f x %.0f) %.90q",
							m.Name, part.Number, b.Hidden, r.X0, r.Y0, r.X1, r.Y1, part.Width, part.Height, b.Text))
					}
				}
			}
			for name := range pageReasons {
				c.reasonPages[name]++
				fileReasons[name] = true
			}
			if text {
				c.textPages++
			}
			for _, w := range part.Warnings {
				switch w.Code {
				case gofastcore.WarnTextHidden:
					if w.Severity == gofastcore.SeverityInfo {
						c.hiddenInfo++
					} else {
						c.hiddenWarning++
					}
				case gofastcore.WarnPartImageOnly:
					c.imageOnly++
				case gofastcore.WarnPartNoText:
					c.noText++
				case gofastcore.WarnPartFailed:
					c.failed++
				case WarnRTLVisualOrder:
					c.rtlPages++
				case WarnDeepHiddenIncomplete:
					c.incompletePages++
				}
			}
			if out.stats.Unjudged > 0 {
				c.unjudgedPages++
			}
			c.unjudgedRuns += out.stats.Unjudged
			c.judgedRuns += out.stats.Judged
			if layer && visible {
				c.mixedLayerPages++
			}
			c.actualDiffers += out.actualDiffers
			c.actualLonger += out.actualLonger
			c.actualDiffersPages += min(out.actualDiffers, 1)
			c.actualLongerPages += min(out.actualLonger, 1)
			c.longGlyphPages += min(out.longGlyph, 1)
			c.tabs += out.stats.Tabs
			c.tabPages += min(out.stats.Tabs, 1)
			c.spacedLines += out.stats.LetterSpaced
			c.spacedPages += min(out.stats.LetterSpaced, 1)
			c.duplicates += out.stats.Duplicates
			c.duplicatePages += min(out.stats.Duplicates, 1)
			c.rejoinedPages += min(out.stats.Rejoined, 1)
			if out.stats.Reordered {
				c.reorderedPages++
			}
		}
		for name := range fileReasons {
			c.reasonFiles[name]++
		}
		if active {
			c.active++
		}
		if external {
			c.external++
		}
		for _, w := range f.Warnings() {
			if w.Code == gofastcore.WarnInputRepaired {
				c.repaired++
				break
			}
		}
		if rebuilt, redefined := f.Rebuilt(); rebuilt {
			c.rebuilt++
			if redefined > 0 {
				c.redefined++
			}
		}
	}

	var sb strings.Builder
	if opts.DeepHidden {
		sb.WriteString("Counts over the public corpus, pages extracted with DeepHidden.\n")
	} else {
		sb.WriteString("Counts over the public corpus, pages extracted with default options.\n")
	}
	sb.WriteString("Scripts and actions outside the pages (catalog, form fields) are not in the active/external counts.\n\n")
	row := func(label string, get func(c *counts) int) {
		fmt.Fprintf(&sb, "%-58s %8d %8d\n", label, get(sets["tune"]), get(sets["held"]))
	}
	fmt.Fprintf(&sb, "%-58s %8s %8s\n", "", "tuning", "held-out")
	row("files in the set", func(c *counts) int { return c.files })
	row("files opened without a password", func(c *counts) int { return c.read })
	row("pages", func(c *counts) int { return c.pages })
	row("pages with text", func(c *counts) int { return c.textPages })
	row("pages with images and no text (part.image_only)", func(c *counts) int { return c.imageOnly })
	row("pages with neither (part.no_text)", func(c *counts) int { return c.noText })
	row("pages that failed (part.failed)", func(c *counts) int { return c.failed })
	sb.WriteString("\nHidden text\n")
	row("pages with text.hidden at info (a text layer only)", func(c *counts) int { return c.hiddenInfo })
	row("pages with text.hidden at warning", func(c *counts) int { return c.hiddenWarning })
	reasons := []string{"invisible", "text_layer", "tiny", "low_contrast", "off_page", "clipped", "occluded", "transparent", "invisible_chars"}
	for _, name := range reasons {
		row("  "+name+": files", func(c *counts) int { return c.reasonFiles[name] })
		row("  "+name+": pages", func(c *counts) int { return c.reasonPages[name] })
		row("  "+name+": blocks", func(c *counts) int { return c.reasonBlocks[name] })
	}
	row("pages with text whose contrast is not judged", func(c *counts) int { return c.unjudgedPages })
	row("  runs whose contrast is not judged", func(c *counts) int { return c.unjudgedRuns })
	row("  runs whose contrast is judged", func(c *counts) int { return c.judgedRuns })
	row("pages with pdf.deep_hidden_incomplete", func(c *counts) int { return c.incompletePages })
	row("pages with a text layer and visible text", func(c *counts) int { return c.mixedLayerPages })
	sb.WriteString("\nProposal E\n")
	row("/ActualText that says other letters than its glyphs: uses", func(c *counts) int { return c.actualDiffers })
	row("  pages with one", func(c *counts) int { return c.actualDiffersPages })
	row("  uses that say 16 bytes more than their glyphs", func(c *counts) int { return c.actualLonger })
	row("  pages with one of those", func(c *counts) int { return c.actualLongerPages })
	row("pages using a font in which one code is over 24 bytes of text", func(c *counts) int { return c.longGlyphPages })
	sb.WriteString("\nLayout\n")
	row("pages with a tab", func(c *counts) int { return c.tabPages })
	row("tabs", func(c *counts) int { return c.tabs })
	row("pages with a line whose letters were closed up", func(c *counts) int { return c.spacedPages })
	row("such lines", func(c *counts) int { return c.spacedLines })
	row("pages with text printed twice", func(c *counts) int { return c.duplicatePages })
	row("runs left out as printed twice", func(c *counts) int { return c.duplicates })
	row("pages with a line put together from pieces drawn apart", func(c *counts) int { return c.rejoinedPages })
	row("pages whose blocks are not in the order drawn", func(c *counts) int { return c.reorderedPages })
	row("pages with right-to-left text (pdf.rtl_visual_order)", func(c *counts) int { return c.rtlPages })
	sb.WriteString("\nFiles\n")
	row("files read through repair (input.repaired)", func(c *counts) int { return c.repaired })
	row("  of them, read by scanning for objects", func(c *counts) int { return c.rebuilt })
	row("  of those, with an object defined more than once", func(c *counts) int { return c.redefined })
	row("files with a script or launch action on a page", func(c *counts) int { return c.active })
	row("files with an external reference on a page", func(c *counts) int { return c.external })
	if err := os.WriteFile(filepath.Join(dir, "evidence.txt"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	var ss strings.Builder
	names := make([]string, 0, len(samples))
	for name := range samples {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&ss, "== %s (tuning set, first %d)\n", name, len(samples[name]))
		for _, s := range samples[name] {
			ss.WriteString(s + "\n")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "samples-tune.txt"), []byte(ss.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + sb.String())
}
