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
	report, summary := render(tools, []int{0, 1}, docs, results)
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
