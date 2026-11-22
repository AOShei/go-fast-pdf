package file

import (
	"fmt"
	"math/rand/v2"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
)

// The problems of a file are kept by code: the first maxPerCode of a code
// in the order of their messages, whatever the order they come in, and
// one warning more that counts the others. A message given again is not
// counted again.
func TestWarnKeepsAndCounts(t *testing.T) {
	var want []gofastcore.Warning
	for round := 0; round < 20; round++ {
		f := &File{}
		r := rand.New(rand.NewPCG(uint64(round), 1))
		msgs := make([]string, 30)
		for i := range msgs {
			msgs[i] = fmt.Sprintf("object %03d: damaged", i)
		}
		r.Shuffle(len(msgs), func(i, j int) { msgs[i], msgs[j] = msgs[j], msgs[i] })
		for _, m := range msgs {
			sev := gofastcore.SeverityWarning
			if m == "object 020: damaged" { // not kept; its severity is the count's
				sev = gofastcore.SeverityError
			}
			f.Warn(sev, "pdf.object_unreadable", m)
			f.Warn(sev, "pdf.object_unreadable", m) // again
		}
		f.Warn(gofastcore.SeverityInfo, "input.repaired", "the table was rebuilt")
		got := f.Warnings()
		if round == 0 {
			want = got
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("round %d: %v, want %v", round, got, want)
		}
	}
	if len(want) != 1+maxPerCode+1 || want[0].Code != "input.repaired" {
		t.Fatalf("%d warnings: %v", len(want), want)
	}
	for i := 0; i < maxPerCode; i++ {
		if w := want[1+i]; w.Message != fmt.Sprintf("object %03d: damaged", i) {
			t.Errorf("kept %d: %q", i, w.Message)
		}
	}
	last := want[len(want)-1]
	if last.Code != "pdf.object_unreadable" || last.Severity != gofastcore.SeverityError ||
		last.Message != fmt.Sprintf("%d more warnings with this code are not listed", 30-maxPerCode) {
		t.Errorf("the count: %+v", last)
	}
}
