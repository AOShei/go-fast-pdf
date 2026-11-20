package gofastpdf

import (
	"testing"
	"time"
)

func TestXMPDate(t *testing.T) {
	plus2 := time.FixedZone("", 2*3600)
	for _, c := range []struct {
		in   string
		want time.Time
	}{
		{"2025-10-21T15:09:24+02:00", time.Date(2025, 10, 21, 15, 9, 24, 0, plus2)},
		{"2025-10-21T15:09:24.5Z", time.Date(2025, 10, 21, 15, 9, 24, 5e8, time.UTC)},
		{"2025-10-21T15:09+02:00", time.Date(2025, 10, 21, 15, 9, 0, 0, plus2)},
		{"2025-10-21T15:09:24", time.Date(2025, 10, 21, 15, 9, 24, 0, time.UTC)},
		{" 2025-10-21 ", time.Date(2025, 10, 21, 0, 0, 0, 0, time.UTC)},
		{"2025-10", time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)},
		{"2025", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"2025-10-21T15:09:24+00:00", time.Date(2025, 10, 21, 15, 9, 24, 0, time.UTC)},
		{"21 October 2025", time.Time{}},
		{"", time.Time{}},
	} {
		got := xmpDate(c.in)
		if !got.Equal(c.want) || got.IsZero() != c.want.IsZero() {
			t.Errorf("%q: got %v, want %v", c.in, got, c.want)
		}
		// The zone is the offset alone, never the zone of this computer.
		if name := got.Location().String(); !got.IsZero() && name != "" && name != "UTC" {
			t.Errorf("%q: in the zone %q", c.in, name)
		}
	}
}
