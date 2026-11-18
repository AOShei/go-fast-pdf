package pdftest

import (
	"strings"
	"unicode"
)

// Score compares extracted text with reference text at word level.
type Score struct {
	// Precision is the share of extracted words that are in the reference;
	// Recall is the share of reference words that were extracted. Both
	// count each word as often as it occurs.
	Precision, Recall, F1 float64
	// Order is the longest common subsequence of the two word sequences
	// divided by the longer length: 1 when the words are the same and in
	// the same order.
	Order float64
	// Got and Want are the word counts.
	Got, Want int
}

var normalizer = strings.NewReplacer(
	"ﬀ", "ff", "ﬁ", "fi", "ﬂ", "fl", "ﬃ", "ffi", "ﬄ", "ffl", "ﬅ", "st", "ﬆ", "st",
	" ", " ", // no-break space
	"­", "", // soft hyphen
	"‐", "-", "‑", "-", // hyphen, non-breaking hyphen
)

// Words splits text into the words the scorer compares: ligatures are
// expanded, soft hyphens removed, and the text is split on white space.
// Case and punctuation are kept.
func Words(text string) []string {
	return strings.FieldsFunc(normalizer.Replace(text), unicode.IsSpace)
}

// Compare scores got against want. Two empty texts score 1.
func Compare(got, want string) Score {
	return ComparePages([]string{got}, []string{want})
}

// ComparePages scores a document page by page and aggregates. Precision
// and recall are computed over the whole document, so a word that lands on
// the wrong page still counts; the order score is computed within each
// page, which keeps the cost quadratic in the page size and not in the
// document size.
func ComparePages(got, want []string) Score {
	var s Score
	counts := map[string][2]int{} // word -> occurrences in {reference, output}
	var lcs, longer int
	for i := 0; i < len(got) || i < len(want); i++ {
		var g, w []string
		if i < len(got) {
			g = Words(got[i])
		}
		if i < len(want) {
			w = Words(want[i])
		}
		s.Got += len(g)
		s.Want += len(w)
		for _, x := range w {
			c := counts[x]
			c[0]++
			counts[x] = c
		}
		for _, x := range g {
			c := counts[x]
			c[1]++
			counts[x] = c
		}
		lcs += lcsLen(g, w)
		longer += max(len(g), len(w))
	}
	// A word seen w times in the reference and g times in the output is
	// matched min(g, w) times.
	matched := 0
	for _, c := range counts {
		matched += min(c[0], c[1])
	}
	if s.Got == 0 && s.Want == 0 {
		return Score{Precision: 1, Recall: 1, F1: 1, Order: 1}
	}
	if s.Got > 0 {
		s.Precision = float64(matched) / float64(s.Got)
	}
	if s.Want > 0 {
		s.Recall = float64(matched) / float64(s.Want)
	}
	if s.Precision+s.Recall > 0 {
		s.F1 = 2 * s.Precision * s.Recall / (s.Precision + s.Recall)
	}
	if longer > 0 {
		s.Order = float64(lcs) / float64(longer)
	}
	return s
}

// lcsLen returns the length of the longest common subsequence of a and b.
// Pages beyond a size limit are compared from a prefix, which keeps a
// pathological page from dominating the test time.
func lcsLen(a, b []string) int {
	const limit = 6000
	if len(a) > limit {
		a = a[:limit]
	}
	if len(b) > limit {
		b = b[:limit]
	}
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	// Intern the words so the inner loop compares integers.
	ids := map[string]int32{}
	intern := func(ws []string) []int32 {
		out := make([]int32, len(ws))
		for i, w := range ws {
			id, ok := ids[w]
			if !ok {
				id = int32(len(ids))
				ids[w] = id
			}
			out[i] = id
		}
		return out
	}
	x, y := intern(a), intern(b)
	prev := make([]int32, len(y)+1)
	cur := make([]int32, len(y)+1)
	for i := range x {
		for j := range y {
			if x[i] == y[j] {
				cur[j+1] = prev[j] + 1
			} else {
				cur[j+1] = max(prev[j+1], cur[j])
			}
		}
		prev, cur = cur, prev
	}
	return int(prev[len(y)])
}
