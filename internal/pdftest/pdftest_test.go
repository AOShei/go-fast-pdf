package pdftest

import (
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// The builder must produce files that an independent parser accepts,
// otherwise a failing library test could be the builder's fault.
func TestBuilderOutputIsValid(t *testing.T) {
	pages := []PageSpec{
		{Content: TextPage("First page", "second line")},
		{Content: TextPage("Second page (with parentheses)")},
	}
	cases := map[string]func(*Builder){
		"classic xref":   func(b *Builder) {},
		"xref stream":    func(b *Builder) { b.XRefStream = true },
		"object streams": func(b *Builder) { b.ObjStm = true },
		"info trailer": func(b *Builder) {
			b.Trailer = "/Info 50 0 R"
			b.Set(50, "<< /Title (Builder test) >>")
		},
	}
	for name, configure := range cases {
		t.Run(name, func(t *testing.T) {
			for _, compress := range []bool{false, true} {
				data := Doc(DocOptions{Compress: compress, Configure: configure}, pages...).Bytes()
				checkValid(t, data, []string{"First page second line", "Second page (with parentheses)"})
			}
		})
	}

	t.Run("incremental update", func(t *testing.T) {
		for _, stream := range []bool{false, true} {
			b := Doc(DocOptions{Configure: func(b *Builder) { b.XRefStream = stream }}, PageSpec{Content: TextPage("Old text")})
			// Object 4 is the first page's content stream.
			b.Update()
			b.Set(4, Stream("", TextPage("New text")))
			checkValid(t, b.Bytes(), []string{"New text"})
		}
	})
}

func checkValid(t *testing.T, data []byte, wantPages []string) {
	t.Helper()
	path := TempFile(t, data)
	if err, ok := QpdfCheck(path); ok && err != nil {
		t.Errorf("builder output rejected: %v", err)
	}
	if !HavePdftotext() {
		return
	}
	pages, err := Pdftotext(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != len(wantPages) {
		t.Fatalf("pdftotext found %d pages, want %d", len(pages), len(wantPages))
	}
	for i, want := range wantPages {
		if got := strings.Join(Words(pages[i]), " "); got != want {
			t.Errorf("page %d: pdftotext read %q, want %q", i+1, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

	s := Compare("the quick brown fox", "the quick brown fox")
	if s.F1 != 1 || s.Order != 1 || s.Precision != 1 || s.Recall != 1 {
		t.Errorf("identical text: %+v, want all 1", s)
	}
	if s := Compare("", ""); s.F1 != 1 || s.Order != 1 {
		t.Errorf("two empty texts: %+v, want 1", s)
	}
	if s := Compare("", "word"); s.F1 != 0 || s.Recall != 0 {
		t.Errorf("empty output: %+v, want 0", s)
	}

	// 4 of 5 extracted words are right, 4 of 6 reference words found.
	s = Compare("the quick brown fax jumps", "the quick brown fox jumps over")
	if !near(s.Precision, 4.0/5) || !near(s.Recall, 4.0/6) || !near(s.F1, 2*(4.0/5)*(4.0/6)/(4.0/5+4.0/6)) {
		t.Errorf("partial match: %+v", s)
	}
	if !near(s.Order, 4.0/6) {
		t.Errorf("order = %v, want %v", s.Order, 4.0/6)
	}

	// Same words, wrong order: F1 stays 1, the order score drops.
	s = Compare("d c b a", "a b c d")
	if s.F1 != 1 || !near(s.Order, 0.25) {
		t.Errorf("reversed text: %+v, want F1 1 and order 0.25", s)
	}

	// Repeated words are counted as often as they occur.
	s = Compare("a a a", "a")
	if !near(s.Precision, 1.0/3) || s.Recall != 1 {
		t.Errorf("repeats: %+v", s)
	}

	// Normalisation: ligatures, no-break space, soft hyphen, line breaks.
	s = Compare("of\ufb01ce\u00a0\u2011 co\u00adoperate", "office\n- cooperate")
	if s.F1 != 1 {
		t.Errorf("normalisation: %+v, want F1 1", s)
	}

	// A word on the wrong page still counts for F1 but not for order.
	s = ComparePages([]string{"a b", "c"}, []string{"a", "b c"})
	if s.F1 != 1 || !near(s.Order, 2.0/4) {
		t.Errorf("pages: %+v, want F1 1 and order 0.5", s)
	}
}

func TestSafelyAndChildFailure(t *testing.T) {
	if err := safely(func() error { panic("boom") }); err == nil || err.Error() != "panic: boom" {
		t.Errorf("safely = %v, want the panic as an error", err)
	}
	if err := safely(func() error { return nil }); err != nil {
		t.Errorf("safely = %v, want nil", err)
	}
	exit := errors.New("exit status 2")
	cases := []struct{ out, want string }{
		{"noise\nDEFECT: got 1 page, want 2\nexit", "got 1 page, want 2"},
		{"runtime: goroutine stack exceeds 67108864-byte limit\nfatal error: stack overflow\n\ngoroutine 1", "runtime: goroutine stack exceeds 67108864-byte limit"},
		{"panic: runtime error: index out of range\n\ngoroutine 7", "panic: runtime error: index out of range"},
		{"something odd", "exit status 2: something odd"},
		{"", "exit status 2"},
	}
	for _, c := range cases {
		if got := childFailure([]byte(c.out), exit); got != c.want {
			t.Errorf("childFailure(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

// The isolation mechanism itself: a clean case passes, and each way of
// dying is reported as an error and not as a dead test binary.
func TestIsolation(t *testing.T) {
	var recurse func(int) int
	recurse = func(n int) int { return recurse(n+1) + 1 }
	cases := map[string]func() error{
		"T/ok":       func() error { return nil },
		"T/error":    func() error { return errors.New("wrong text") },
		"T/panic":    func() error { panic("boom") },
		"T/overflow": func() error { recurse(0); return nil },
		"T/hang":     func() error { time.Sleep(time.Hour); return nil },
	}
	if fn, ok := cases[os.Getenv(isolatedEnv)]; ok {
		childMain(fn)
	}
	run := func(key string, timeout time.Duration) string {
		if err := runChild(t, key, timeout); err != nil {
			return err.Error()
		}
		return ""
	}
	if got := run("T/ok", time.Minute); got != "" {
		t.Errorf("clean case: %v", got)
	}
	if got := run("T/error", time.Minute); got != "wrong text" {
		t.Errorf("error case: %q", got)
	}
	if got := run("T/panic", time.Minute); got != "panic: boom" {
		t.Errorf("panic case: %q", got)
	}
	if got := run("T/overflow", time.Minute); !strings.Contains(got, "stack") {
		t.Errorf("stack overflow case: %q", got)
	}
	if got := run("T/hang", 2*time.Second); !strings.Contains(got, "hang") {
		t.Errorf("hang case: %q", got)
	}
}
