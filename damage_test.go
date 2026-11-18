package gofastpdf_test

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// The damage suite (IMPLEMENTATION_PLAN.md, Phase 2): valid files are
// damaged in the ways files are damaged in practice, and each must still
// give the text of the undamaged file.

var lengthEntry = regexp.MustCompile(`/Length (\d+)`)

// damages returns the mutations. Each takes a valid file and returns the
// damaged one, or nil if the mutation does not apply to that file.
func damages() map[string]func([]byte) []byte {
	replaceLengths := func(data []byte, with func(digits int) string) []byte {
		// The replacement has the width of the original, so that no
		// offset in the file changes and only the lengths are wrong.
		return lengthEntry.ReplaceAllFunc(data, func(m []byte) []byte {
			digits := len(m) - len("/Length ")
			return []byte("/Length " + with(digits))
		})
	}
	return map[string]func([]byte) []byte{
		"startxref points nowhere": func(d []byte) []byte {
			i := bytes.LastIndex(d, []byte("startxref"))
			return append(append([]byte{}, d[:i]...), "startxref\n7\n%%EOF\n"...)
		},
		"every startxref removed": func(d []byte) []byte {
			return bytes.ReplaceAll(d, []byte("startxref"), []byte("startxxxx"))
		},
		"cross-reference data deleted": func(d []byte) []byte {
			// Cut at the last classic table, or at the last
			// cross-reference stream object.
			if i := bytes.LastIndex(d, []byte("\nxref")); i >= 0 && bytes.LastIndex(d, []byte("/Type /XRef")) < i {
				return append([]byte{}, d[:i+1]...)
			}
			i := bytes.LastIndex(d, []byte("/XRef"))
			if i < 0 {
				return nil
			}
			i = bytes.LastIndex(d[:i], []byte(" 0 obj"))
			i = bytes.LastIndexAny(d[:i], "\r\n") + 1
			// A linearized file has its first-page section early in the
			// file; cutting there would remove the pages.
			if i < len(d)/2 {
				return nil
			}
			return append([]byte{}, d[:i]...)
		},
		"last kilobyte cut off": func(d []byte) []byte {
			// Only where no stream ends in that kilobyte: the test is
			// about the cross-reference data, not about lost content.
			if len(d) < 4096 || bytes.Contains(d[len(d)-1024:], []byte("endstream")) {
				return nil
			}
			return append([]byte{}, d[:len(d)-1024]...)
		},
		"100 bytes before the header": func(d []byte) []byte {
			return append(bytes.Repeat([]byte("junk mail header\r\n"), 6)[:100], d...)
		},
		"5000 bytes before the header": func(d []byte) []byte {
			return append(bytes.Repeat([]byte{' '}, 5000), d...)
		},
		"4000 bytes after the end": func(d []byte) []byte {
			return append(append([]byte{}, d...), bytes.Repeat([]byte{0}, 4000)...)
		},
		"a comment inserted after the first object": func(d []byte) []byte {
			i := bytes.Index(d, []byte("endobj"))
			if i < 0 {
				return nil
			}
			i += len("endobj")
			return append(append(append([]byte{}, d[:i]...), "\n% inserted by an editor\n"...), d[i:]...)
		},
		"every /Length set to 0": func(d []byte) []byte {
			return replaceLengths(d, func(n int) string { return "0" + strings.Repeat(" ", n-1) })
		},
		"every /Length too large": func(d []byte) []byte {
			return replaceLengths(d, func(n int) string { return strings.Repeat("9", n) })
		},
		"every /Length too small": func(d []byte) []byte {
			return replaceLengths(d, func(n int) string { return "1" + strings.Repeat(" ", n-1) })
		},
		"line ends converted to CR LF in the table": func(d []byte) []byte {
			i := bytes.LastIndex(d, []byte("\nxref\n"))
			j := bytes.LastIndex(d, []byte("trailer"))
			if i < 0 || j < i || bytes.LastIndex(d, []byte("/Type /XRef")) > i {
				return nil
			}
			// 20-byte entries ending in space and line feed become
			// 21-byte entries.
			table := bytes.ReplaceAll(d[i:j], []byte(" \n"), []byte(" \r\n"))
			out := append(append([]byte{}, d[:i]...), table...)
			return append(out, d[j:]...)
		},
	}
}

func TestDamageSuite(t *testing.T) {
	fixtures := []string{
		"english", "english-objstm", "english-linearized", "english-uncompressed",
		"latin1", "multilingual", "multilingual-objstm-linearized",
		"enc-rc4-128", "enc-aes-128",
	}
	for _, name := range fixtures {
		good, err := os.ReadFile(fixture(name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := extractBytes(good, extractOpts{})
		if err != nil {
			t.Fatalf("%s: the undamaged file does not extract: %v", name, err)
		}
		for dname, damage := range damages() {
			t.Run(name+"/"+dname, func(t *testing.T) {
				bad := damage(good)
				if bad == nil {
					t.Skip("does not apply to this file")
				}
				if bytes.Equal(bad, good) {
					t.Fatal("the mutation did not change the file")
				}
				// An encrypted file's lengths cannot be found by looking
				// for endstream once the key is lost, and its key is in
				// the trailer.
				if strings.HasPrefix(name, "enc-") && (dname == "cross-reference data deleted" || dname == "last kilobyte cut off") {
					t.Skip("the trailer with the encryption dictionary is in the part removed")
				}
				// The encryption dictionary has a /Length of its own, the
				// key length, which the mutation would change as well.
				if strings.HasPrefix(name, "enc-") && strings.HasPrefix(dname, "every /Length") {
					t.Skip("would also change the key length")
				}
				got, err := extractBytes(bad, extractOpts{})
				if err != nil {
					t.Fatalf("extraction failed: %v", err)
				}
				if len(got.Pages) != len(want.Pages) {
					t.Fatalf("%d pages, want %d", len(got.Pages), len(want.Pages))
				}
				for i := range want.Pages {
					if got.Pages[i].Text != want.Pages[i].Text {
						t.Fatalf("page %d differs: starts %.50q, want %.50q", i+1, got.Pages[i].Text, want.Pages[i].Text)
					}
				}
			})
		}
	}
}

// A Flate stream cut in half keeps what could be decompressed before the
// cut: the text of a damaged page starts like the original.
func TestDamageFlateCutInHalf(t *testing.T) {
	var lines []string
	for i := range 300 {
		lines = append(lines, fmt.Sprintf("line %03d of a page that will lose its second half", i))
	}
	content := pdftest.TextPage(lines...)
	good := pdftest.Doc(pdftest.DocOptions{Compress: true}, pdftest.PageSpec{Content: content}, pdftest.PageSpec{Content: pdftest.TextPage("the next page is whole")})
	want, err := extractBytes(good.Bytes(), extractOpts{})
	if err != nil {
		t.Fatal(err)
	}
	z := pdftest.Deflate([]byte(content))
	for _, keep := range []int{len(z) / 2, len(z) / 10, len(z) - 5} {
		bad := pdftest.Doc(pdftest.DocOptions{Compress: true}, pdftest.PageSpec{}, pdftest.PageSpec{Content: pdftest.TextPage("the next page is whole")})
		bad.Set(4, pdftest.Stream("/Filter /FlateDecode", string(z[:keep])))
		got, err := extractBytes(bad.Bytes(), extractOpts{})
		if err != nil {
			t.Fatalf("keep %d: %v", keep, err)
		}
		gotLines := strings.Split(got.Pages[0].Text, "\n")
		wantLines := strings.Split(want.Pages[0].Text, "\n")
		// All lines but the last, which may be cut, are lines of the
		// original, in order. With half of the compressed data or more,
		// a fair share of them is there; with a tenth, the decompressor
		// may not have had its tables yet.
		n := len(gotLines) - 1
		minimum := 0
		if keep >= len(z)/2 {
			minimum = len(wantLines) / 5
		}
		if n < minimum || n > len(wantLines) {
			t.Errorf("keep %d of %d bytes: %d lines of %d, want at least %d", keep, len(z), n, len(wantLines), minimum)
			continue
		}
		for i := 0; i < n; i++ {
			if gotLines[i] != wantLines[i] {
				t.Errorf("keep %d: line %d is %q, want %q", keep, i, gotLines[i], wantLines[i])
				break
			}
		}
		if got.Pages[1].Text != want.Pages[1].Text {
			t.Errorf("keep %d: the page after the damaged one reads %q", keep, got.Pages[1].Text)
		}
	}
}
