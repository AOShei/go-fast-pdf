package main

import (
	"strings"
	"testing"
)

func TestBetween(t *testing.T) {
	text := "before\n" + beginMarker + "\nold table\n" + endMarker + "\nafter\n"
	got, err := between(text, "new table\n")
	if err != nil || got != "before\n"+beginMarker+"\nnew table\n"+endMarker+"\nafter\n" {
		t.Errorf("%q, %v", got, err)
	}
	// Doing it again changes nothing.
	if again, _ := between(got, "new table\n"); again != got {
		t.Errorf("a second time: %q", again)
	}
	for _, bad := range []string{"no markers", endMarker + beginMarker, beginMarker} {
		if _, err := between(bad, "x"); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestNumbers(t *testing.T) {
	for v, want := range map[float64]string{0.1284: "0.128", 1.706: "1.71", 13.04: "13.0", 427.5: "428", 2160.5: "2,161", 1234567: "1,234,567"} {
		if got := number(v); got != want {
			t.Errorf("number(%v) = %q, want %q", v, got, want)
		}
	}
}

// The summary counts only the documents that every program read, and
// says of a program how many it failed on.
func TestRender(t *testing.T) {
	tools := []tool{{name: "ours", note: "library"}, {name: "theirs", note: "command"}, {name: "left out"}}
	docs := []document{{name: "a.pdf", pages: 10, bytes: 1 << 20}, {name: "b.pdf", pages: 90, bytes: 3 << 20}}
	results := [][]result{
		{{Seconds: 0.001, Chars: 100, Peak: 20 << 20}, {Seconds: 0.009, Chars: 900, Peak: 30 << 20}},
		{{Seconds: 0.004, Chars: 101, Version: "1.2"}, {Err: "failed: no result"}},
		nil,
	}
	report, summary := render(tools, []int{0, 1}, docs, results, -1, agreement{})
	for _, want := range []string{
		"| ours | 10,000 | 1 | 30 MB |",                     // 10 pages in a millisecond: b.pdf does not count
		"| theirs (failed on 1) | 2,500 | 4.00 times | - |", // and no memory was reported
		"1 documents, 10 pages, 1.0 MB",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary has no %q:\n%s", want, summary)
		}
	}
	for _, want := range []string{summary, "| theirs | 1.2 | command |", "| b.pdf | 90 | 9.00 | failed: no result |", "| b.pdf | 900 | - |"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report has no %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "left out") {
		t.Error("a program that was not measured is in the report")
	}
}

// With a reference, the summary gives the mean F1 over the documents every
// program read, the reference says it is the reference, and the report
// has the F1 of each document.
func TestRenderWords(t *testing.T) {
	tools := []tool{{name: "ours"}, {name: "reference"}, {name: "other"}}
	docs := []document{{name: "a.pdf", pages: 10}, {name: "b.pdf", pages: 10}, {name: "c.pdf", pages: 10}}
	results := [][]result{
		{{Seconds: 0.001, F1: 0.9, Scored: true}, {Seconds: 0.001, F1: 0.8, Scored: true}, {Seconds: 0.001, F1: 0.1, Scored: true}},
		{{Seconds: 0.002, F1: 1, Scored: true}, {Seconds: 0.002, F1: 1, Scored: true}, {Seconds: 0.002, F1: 1, Scored: true}},
		{{Seconds: 0.003, F1: 0.5, Scored: true}, {Seconds: 0.003}, {Err: "failed: no result"}},
	}
	report, summary := render(tools, []int{0, 1, 2}, docs, results, 1, agreement{})
	for _, want := range []string{
		"| Words as pdftotext's (F1) |",
		"| ours | 10,000 | 1 | - | 0.850 |", // c.pdf, which other failed on, does not count
		"| reference | 5,000 | 2.00 times | - | the reference |",
		"| other (failed on 1) | 3,333 | 3.00 times | - | 0.500 |", // b.pdf was not scored
		"not how correct they are",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary has no %q:\n%s", want, summary)
		}
	}
	for _, want := range []string{"| Document | ours | other |", "| a.pdf | 0.900 | 0.500 |", "| b.pdf | 0.800 | - |", "| c.pdf | 0.100 | - |"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report has no %q:\n%s", want, report)
		}
	}
}

// Every pair of programs is compared over the documents every program
// read, both ways at once; a program that gives go-fast-pdf's text again
// has no row, and the report has the table with the mean of each row.
func TestAgreement(t *testing.T) {
	tools := []tool{{name: "ours"}, {name: "ours again", again: true}, {name: "theirs"}, {name: "other"}}
	docs := []document{{name: "a.pdf"}, {name: "b.pdf"}}
	results := [][]result{
		{{Seconds: 1, Text: "one two three four"}, {Seconds: 1, Text: "x"}},
		{{Seconds: 1, Text: "one two three four"}, {Seconds: 1, Text: "x"}},
		{{Seconds: 1, Text: "one two three five"}, {Seconds: 1, Text: "y"}},
		{{Seconds: 1, Text: "one two"}, {Err: "failed: no result"}}, // b.pdf does not count
	}
	kept := []int{0, 1, 2, 3}
	a := agreeAll(tools, kept, docs, results)
	if len(a.programs) != 3 || a.programs[0] != 0 || a.programs[1] != 2 || a.programs[2] != 3 {
		t.Fatalf("programs %v", a.programs)
	}
	for _, c := range []struct {
		i, j int
		want float64
	}{{0, 1, 0.75}, {1, 0, 0.75}, {0, 2, 2.0 / 3}, {2, 0, 2.0 / 3}, {0, 0, 0}} {
		if got := a.f1[c.i][c.j]; got < c.want-1e-9 || got > c.want+1e-9 {
			t.Errorf("f1[%d][%d] = %v, want %v", c.i, c.j, got, c.want)
		}
	}
	report, summary := render(tools, kept, docs, results, -1, a)
	for _, want := range []string{
		"| | ours | theirs | other | Mean against the others |",
		"| **ours** | - | 0.750 | 0.667 | 0.708 |",
		"| **other** | 0.667 | 0.667 | - | 0.667 |",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report has no %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "**ours again**") {
		t.Error("the program that gives the same text has a row")
	}
	if !strings.Contains(summary, "how far each pair of programs agree") {
		t.Errorf("the summary does not point to the table:\n%s", summary)
	}
}
