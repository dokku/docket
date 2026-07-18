package tasks

import (
	"math"
	"testing"
)

func TestSafeCap(t *testing.T) {
	cases := []struct {
		name  string
		base  int
		extra int
		want  int
	}{
		{"both zero", 0, 0, 0},
		{"small sum", 3, 4, 7},
		{"zero extra", 5, 0, 5},
		{"doubling shape", 6, 6, 12},
		{"boundary sum", math.MaxInt - 1, 1, math.MaxInt},
		{"overflow returns base", math.MaxInt, 1, math.MaxInt},
		{"huge extra returns base", 10, math.MaxInt, 10},
		{"negative extra returns base", 5, -1, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeCap(tc.base, tc.extra); got != tc.want {
				t.Errorf("safeCap(%d, %d) = %d, want %d", tc.base, tc.extra, got, tc.want)
			}
		})
	}
}

// TestSafeCapAllocates ensures the returned hint is a legal make() capacity
// for realistic and boundary inputs.
func TestSafeCapAllocates(t *testing.T) {
	for _, base := range []int{0, 8, 1024} {
		s := make([]int, 0, safeCap(base, 2))
		if cap(s) < base {
			t.Errorf("cap(make with safeCap(%d, 2)) = %d, want >= %d", base, cap(s), base)
		}
	}
	// The unreachable-overflow branch must still yield an allocatable hint.
	if got := safeCap(math.MaxInt, 4); got != math.MaxInt {
		t.Fatalf("safeCap(MaxInt, 4) = %d, want MaxInt", got)
	}
	m := make(map[string]int, safeCap(0, 4))
	if len(m) != 0 {
		t.Errorf("new map should be empty, got len %d", len(m))
	}
}
