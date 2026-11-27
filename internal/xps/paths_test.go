package xps

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/extract"
	"github.com/AOShei/go-fast-pdf/internal/xpstest"
)

// content is what a page hands to the layout, copied.
type content struct {
	text   string
	runs   []string
	rules  []extract.Rule
	fills  []extract.Rule
	images []extract.Image
	res    extract.Result
	info   PageInfo
}

// read reads the first page of a package.
func read(t *testing.T, pkg *xpstest.Package) content {
	t.Helper()
	data := pkg.Bytes()
	d, err := Open(bytes.NewReader(data), int64(len(data)), Limits{MaxDecodedBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	var c content
	var w Worker
	err = d.ReadPage(0, extract.Options{Rules: true, Use: func(x *extract.Content) {
		c.text = string(x.Text)
		for _, r := range x.Runs {
			c.runs = append(c.runs, string(x.Text[r.Off:r.Off+r.Len]))
		}
		c.rules = append(c.rules, x.Rules...)
		c.fills = append(c.fills, x.Fills...)
		c.images = append(c.images, x.Images...)
	}}, &c.res, &w, &c.info)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func page(body string) *xpstest.Package {
	return xpstest.Document(xpstest.XPS, xpstest.Page(xpstest.XPS, 816, 1056, body))
}

func boxes(rs []extract.Rule) string {
	var out []string
	for _, r := range rs {
		out = append(out, fmt.Sprintf("%.1f,%.1f,%.1f,%.1f", r[0], r[1], r[2], r[3]))
	}
	return strings.Join(out, " ")
}

// TestPathRules: the painted figures of a path that run along the axes
// are rules when they are thin and filled boxes when they are not; what
// is declared to paint nothing is neither, and neither is a curve.
func TestPathRules(t *testing.T) {
	// A page of 816 by 1056 units is 612 by 792 points, y upwards.
	c := read(t, page(
		// A thin filled box: a rule, 300 units long and 1 thick.
		`<Path Data="M 100,200 L 400,200 L 400,201 L 100,201 Z" Fill="#FF000000"/>`+
			// A stroked line, 2 units thick.
			`<Path Data="M 100,300 L 100,500" Stroke="#FF000000" StrokeThickness="2"/>`+
			// A filled box that is thick: the ground of a cell, perhaps.
			`<Path Data="M 100,600 H 400 V 700 H 100 Z"><Path.Fill><SolidColorBrush Color="#FFEEEEEE"/></Path.Fill></Path>`+
			// Small letters are from the current point.
			`<Path Data="m 500,200 l 100,0 0,1 -100,0 z" Fill="#FF000000"/>`))
	if got, want := boxes(c.rules), "75.0,641.2,300.0,642.0 74.2,417.0,75.8,567.0 375.0,641.2,450.0,642.0"; got != want {
		t.Errorf("rules: %s\nwant:  %s", got, want)
	}
	if got, want := boxes(c.fills), "75.0,267.0,300.0,342.0"; got != want {
		t.Errorf("fills: %s, want %s", got, want)
	}

	// What paints nothing, by what the file declares: no fill and no
	// stroke, a colour without alpha, an opacity of nothing, on the path
	// or around it. And what does not run along the axes.
	c = read(t, page(
		`<Path Data="M 100,200 L 400,200 L 400,201 L 100,201 Z"/>`+
			`<Path Data="M 100,210 L 400,210 L 400,211 L 100,211 Z" Fill="#00000000"/>`+
			`<Path Data="M 100,220 L 400,220 L 400,221 L 100,221 Z" Fill="#FF000000" Opacity="0"/>`+
			`<Canvas Opacity="0"><Path Data="M 100,230 L 400,230 L 400,231 L 100,231 Z" Fill="#FF000000"/></Canvas>`+
			`<Path Data="M 100,240 L 400,240 L 400,241 L 100,241 Z"><Path.Fill><SolidColorBrush Color="#FF000000" Opacity="0"/></Path.Fill></Path>`+
			`<Path Data="M 100,250 L 400,250" Stroke="sc#0,0,0,0"/>`+
			`<Path Data="M 100,300 L 400,400 L 100,400 Z" Fill="#FF000000"/>`+
			`<Path Data="M 100,500 C 200,500 300,600 400,600 L 100,600 Z" Fill="#FF000000"/>`+
			`<Path Data="M 100,700 L 400,700 L 400,701 L 100,701 Z" Fill="#FF000000" RenderTransform="0.7,0.7,-0.7,0.7,0,0"/>`))
	if len(c.rules) != 0 || len(c.fills) != 0 {
		t.Errorf("rules %s; fills %s", boxes(c.rules), boxes(c.fills))
	}

	// Through the transformations around it, and its own.
	c = read(t, page(`<Canvas RenderTransform="2,0,0,2,0,0"><Path Data="M 0,0 L 150,0 L 150,0.5 L 0,0.5 Z" Fill="#FF000000" RenderTransform="1,0,0,1,50,100"/></Canvas>`))
	if got, want := boxes(c.rules), "75.0,641.2,300.0,642.0"; got != want {
		t.Errorf("transformed: %s, want %s", got, want)
	}
}

// TestPathGeometryElements: the figures of a path written as elements.
func TestPathGeometryElements(t *testing.T) {
	c := read(t, page(
		`<Path Fill="#FF000000"><Path.Data><PathGeometry><PathFigure StartPoint="100,200" IsClosed="true">`+
			`<PolyLineSegment Points="400,200 400,201 100,201"/></PathFigure></PathGeometry></Path.Data></Path>`+
			`<Path Stroke="#FF000000"><Path.Data><PathGeometry Figures="M 0,0 L 0,200"><PathGeometry.Transform><MatrixTransform Matrix="1,0,0,1,100,300"/></PathGeometry.Transform></PathGeometry></Path.Data></Path>`+
			`<Path Fill="#FF000000"><Path.Data><PathGeometry><PathFigure StartPoint="100,600"><PolyBezierSegment Points="150,600 200,650 250,650"/><PolyLineSegment Points="100,650"/></PathFigure></PathGeometry></Path.Data></Path>`))
	if got, want := boxes(c.rules), "75.0,641.2,300.0,642.0 74.6,417.0,75.4,567.0"; got != want {
		t.Errorf("rules: %s\nwant:  %s", got, want)
	}
	if len(c.fills) != 0 {
		t.Errorf("fills: %s", boxes(c.fills))
	}
}

// TestImages: a path filled with an image is where the image is, with
// what the file says it shows.
func TestImages(t *testing.T) {
	pkg := page(
		`<Path Data="M 96,96 L 296,96 L 296,196 L 96,196 Z" AutomationProperties.HelpText="A chart of sales"><Path.Fill>` +
			`<ImageBrush ImageSource="/Resources/Images/a.png" Viewbox="0,0,10,5" Viewport="0,0,1,1" TileMode="None" ViewboxUnits="Absolute" ViewportUnits="Absolute">` +
			`<ImageBrush.Transform><MatrixTransform Matrix="200,0,0,100,96,96"/></ImageBrush.Transform></ImageBrush></Path.Fill></Path>` +
			`<Path Data="M 96,300 L 196,300 L 196,400 L 96,400 Z" Fill="{StaticResource none}"/>` +
			`<Path Data="M 96,500 L 196,500 L 196,600 L 96,600 Z"><Path.Fill><ImageBrush ImageSource="{ColorConvertedBitmap ../../../Resources/Images/a.png /p.icc}" Viewbox="0,0,1,1" Viewport="0,0,1,1"/></Path.Fill></Path>` +
			`<Path Data="M 96,700 L 196,700 L 196,800 L 96,800 Z"><Path.Fill><ImageBrush ImageSource="/Resources/Images/missing.png" Viewbox="0,0,1,1" Viewport="0,0,1,1"/></Path.Fill></Path>` +
			`<Path Data="M 96,800 L 196,800 L 196,900 L 96,900 Z"><Path.Fill><ImageBrush ImageSource="http://example.com/a.png" Viewbox="0,0,1,1" Viewport="0,0,1,1"/></Path.Fill></Path>`)
	pkg.Add("Resources/Images/a.png", []byte("\x89PNG\r\n\x1a\n"))
	c := read(t, pkg)
	if len(c.images) != 2 {
		t.Fatalf("%d images", len(c.images))
	}
	img := c.images[0]
	if img.Name != "resources/images/a.png" || img.Alt != "A chart of sales" {
		t.Errorf("image: %q %q", img.Name, img.Alt)
	}
	if got, want := fmt.Sprintf("%.1f %.1f %.1f %.1f", img.Rect[0], img.Rect[1], img.Rect[2], img.Rect[3]), "72.0 645.0 150.0 75.0"; got != want {
		t.Errorf("rect: %s, want %s", got, want)
	}
	if c.images[1].Name != "resources/images/a.png" {
		t.Errorf("an image with a colour profile: %q", c.images[1].Name)
	}
	// An image is no ground of a cell; one outside the package is noted.
	if len(c.fills) != 0 {
		t.Errorf("fills: %s", boxes(c.fills))
	}
	if !c.info.External {
		t.Error("an image outside the package is not noted")
	}
}

// TestResources: the names a resource dictionary gives, in the page, in
// a canvas, where they end, and in a part of their own.
func TestResources(t *testing.T) {
	g := func(y float64, text, extra string) string { return xpstest.Glyphs(96, y, 16, text, extra) }
	dict := `<ResourceDictionary xmlns="` + xpstest.XPS + `" xmlns:x="` + xpstest.XPS + `/resourcedictionary-key">` +
		`<SolidColorBrush x:Key="ink" Color="#FF000000"/>` +
		`<PathGeometry x:Key="line" Figures="M 100,200 L 400,200 L 400,201 L 100,201 Z"/>` +
		`</ResourceDictionary>`
	body := `<FixedPage.Resources><ResourceDictionary Source="/Resources/shared.dict"/></FixedPage.Resources>` +
		`<Canvas><Canvas.Resources><ResourceDictionary xmlns:x="` + xpstest.XPS + `/resourcedictionary-key">` +
		`<MatrixTransform x:Key="shift" Matrix="1,0,0,1,0,100"/>` +
		`<SolidColorBrush x:Key="none" Color="#00FFFFFF"/>` +
		`<SolidColorBrush x:Key="ink" Color="#00000000"/>` +
		`</ResourceDictionary></Canvas.Resources>` +
		// In the canvas "ink" is the one of the canvas, which is clear.
		`<Path Data="{StaticResource line}" Fill="{StaticResource ink}"/>` +
		`<Path Data="{StaticResource line}" Fill="#FF000000" RenderTransform="{StaticResource shift}"/>` +
		`</Canvas>` +
		// After it, "ink" is the page's again, and "shift" is no more.
		`<Path Data="{StaticResource line}" Fill="{StaticResource ink}"/>` +
		`<Path Data="M 100,500 L 400,500 L 400,501 L 100,501 Z" Fill="{StaticResource none}"/>` +
		`<Path Data="{StaticResource nothing}" Fill="#FF000000"/>`
	pkg := page(body)
	pkg.Add("Resources/shared.dict", []byte(dict))
	c := read(t, pkg)
	if got, want := boxes(c.rules), "75.0,566.2,300.0,567.0 75.0,641.2,300.0,642.0"; got != want {
		t.Errorf("rules: %s\nwant:  %s", got, want)
	}
	if c.info.External || len(c.res.Warnings) != 0 {
		t.Errorf("external %v, warnings %v", c.info.External, c.res.Warnings)
	}

	// A fill of text by a resource: drawn, or declared clear.
	pkg = page(`<FixedPage.Resources><ResourceDictionary xmlns:x="` + xpstest.XPS + `/resourcedictionary-key">` +
		`<SolidColorBrush x:Key="ink" Color="#FF000000"/><SolidColorBrush x:Key="none" Color="#00000000"/>` +
		`<MatrixTransform x:Key="shift" Matrix="1,0,0,1,96,0"/></ResourceDictionary></FixedPage.Resources>` +
		strings.Replace(g(100, "Drawn.", ""), `Fill="#FF000000"`, `Fill="{StaticResource ink}" RenderTransform="{StaticResource shift}"`, 1) +
		strings.Replace(g(200, "Not drawn.", ""), `Fill="#FF000000"`, `Fill="{StaticResource none}"`, 1))
	d := pkg.Bytes()
	doc, err := Open(bytes.NewReader(d), int64(len(d)), Limits{MaxDecodedBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	var w Worker
	var res extract.Result
	var info PageInfo
	var modes []uint8
	var alphas []float32
	var xs []float64
	doc.ReadPage(0, extract.Options{Use: func(x *extract.Content) {
		for _, r := range x.Runs {
			modes, alphas, xs = append(modes, r.Mode), append(alphas, r.FillAlpha), append(xs, r.X0)
		}
	}}, &res, &w, &info)
	if fmt.Sprint(modes, alphas, xs) != "[0 0] [1 0] [144 72]" {
		t.Errorf("modes %v, alphas %v, at %v", modes, alphas, xs)
	}

	// A dictionary outside the package is not fetched, and is noted.
	c = read(t, page(`<FixedPage.Resources><ResourceDictionary Source="http://example.com/shared.dict"/></FixedPage.Resources>`+g(100, "Text.", "")))
	if !c.info.External || c.text != "Text." {
		t.Errorf("external %v, text %q", c.info.External, c.text)
	}

	// Dictionaries that name each other end.
	pkg = page(`<FixedPage.Resources><ResourceDictionary Source="/a.dict"/></FixedPage.Resources>` + g(100, "Text.", ""))
	pkg.Add("a.dict", []byte(`<ResourceDictionary xmlns="`+xpstest.XPS+`" Source="/a.dict"/>`))
	c = read(t, pkg)
	if c.text != "Text." {
		t.Errorf("text %q", c.text)
	}
	found := false
	for _, wn := range c.res.Warnings {
		found = found || wn.Code == gofastcore.WarnLimitExceeded
	}
	if !found {
		t.Errorf("warnings: %v", c.res.Warnings)
	}
}

// TestVisualBrush: what a visual brush is drawn with is on the page
// where the brush paints, and its text is text of the page.
func TestVisualBrush(t *testing.T) {
	inner := `<Glyphs Fill="#FF000000" FontUri="` + xpstest.FontName + `" FontRenderingEmSize="10" OriginX="0" OriginY="10" UnicodeString="In a brush."/>` +
		`<Path Data="M 0,20 L 100,20 L 100,20.5 L 0,20.5 Z" Fill="#FF000000"/>`
	c := read(t, page(
		`<Path Data="M 96,96 L 296,96 L 296,196 L 96,196 Z"><Path.Fill>`+
			`<VisualBrush Viewbox="0,0,100,50" Viewport="96,96,200,100" ViewboxUnits="Absolute" ViewportUnits="Absolute" TileMode="None">`+
			`<VisualBrush.Visual><Canvas>`+inner+`</Canvas></VisualBrush.Visual></VisualBrush></Path.Fill></Path>`+
			xpstest.Glyphs(96, 400, 16, "After it.", "")))
	if c.text != "In a brush.After it." && c.text != "In a brush.\nAfter it." && strings.Join(c.runs, "|") != "In a brush.|After it." {
		t.Errorf("text %q, runs %q", c.text, c.runs)
	}
	// The content is twice as large in the brush: its rule is 200 units
	// long, from 96, 136 to 137 units from the top.
	if got, want := boxes(c.rules), "72.0,689.2,222.0,690.0"; got != want {
		t.Errorf("rules: %s, want %s", got, want)
	}
}

// FuzzPathData reads figures in the abbreviated syntax: nothing is read
// for ever, and what is kept is bounded.
func FuzzPathData(f *testing.F) {
	for _, seed := range []string{
		"M 100,200 L 400,200 L 400,201 L 100,201 Z",
		"F1 m 500,200 l 100,0 0,1 -100,0 z M0,0H10V10H0Z",
		"M 100,500 C 200,500 300,600 400,600 S 1,2 3,4 Q 1 2 3 4 A 5,5 0 1 0 10,10 L 100,600 Z",
		"M1e3-2.5.5L+1-1,,,X",
	} {
		f.Add(seed, false)
	}
	f.Fuzz(func(t *testing.T, data string, transformed bool) {
		var g geometry
		ops := 0
		fig := figures{g: &g, ops: &ops, follow: true}
		if transformed {
			fig.m, fig.transformed = matrix{0, 1, -1, 0, 5, 5}, true
		}
		fig.abbreviated([]byte(data))
		if len(g.segs) > len(data) || len(g.polys) > len(data) || ops > len(data) {
			t.Fatalf("%d segments, %d figures, %d operations of %d bytes", len(g.segs), len(g.polys), ops, len(data))
		}
		for _, b := range append(g.segs, g.polys...) {
			for _, v := range b {
				if v != v {
					t.Fatal("a box that is not a number")
				}
			}
		}
	})
}
