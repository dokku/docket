package commands

import "math"

// safeCap returns base+extra for use as a make() capacity hint while guarding
// the addition against signed-integer overflow. Callers pass lengths of
// in-memory collections, which are non-negative and far below math.MaxInt, so
// the guard never trips at runtime; it exists so the capacity arithmetic is
// provably non-overflowing (resolving CodeQL go/allocation-size-overflow) and
// degrades to a valid hint instead of wrapping negative on pathological input.
// On the unreachable overflow it returns base, itself a valid length, so
// make() still receives a sane hint and grows as needed.
func safeCap(base, extra int) int {
	if extra < 0 || base > math.MaxInt-extra {
		return base
	}
	return base + extra
}
