// Package num provides small numeric predicates shared across packages.
package num

import "math"

// Finite reports whether v is neither NaN nor an infinity. The Go standard
// library exposes IsNaN/IsInf but no IsFinite, so this keeps the idiom in one
// place and makes validation messages consistent.
func Finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
