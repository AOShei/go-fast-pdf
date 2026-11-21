package layout

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// sortStable gives the order that slices.SortStableFunc gives with a cmp
// that asks less both ways, for orders that are consistent and for ones
// that are not (geometry can make a less that is not transitive): the
// sort asks only whether cmp(a, b) < 0. Were that to change, this fails.
func TestSortStableAsksLessOnce(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	for round := 0; round < 3000; round++ {
		n := r.IntN(120)
		keys := make([]int, n)
		for i := range keys {
			keys[i] = r.IntN(1 + n/3)
		}
		var less func(a, b int32) bool
		switch round % 3 {
		case 0: // by key, with ties
			less = func(a, b int32) bool { return keys[a] < keys[b] }
		case 1: // not transitive: within a distance only
			less = func(a, b int32) bool { return keys[a] < keys[b] && keys[b]-keys[a] < 3 }
		default: // no order at all
			seed := r.Uint64()
			less = func(a, b int32) bool { return (uint64(a)*31+uint64(b)*17+seed)%5 < 2 }
		}
		s := make([]int32, n)
		for i := range s {
			s[i] = int32(r.IntN(n))
		}
		want := slices.Clone(s)
		slices.SortStableFunc(want, func(a, b int32) int {
			switch {
			case less(a, b):
				return -1
			case less(b, a):
				return 1
			}
			return 0
		})
		sortStable(s, less)
		if !slices.Equal(s, want) {
			t.Fatalf("round %d: %v, want %v", round, s, want)
		}
	}
}
