package commands

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
		{"boundary sum", math.MaxInt - 1, 1, math.MaxInt},
		{"overflow returns base", math.MaxInt, 4, math.MaxInt},
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
