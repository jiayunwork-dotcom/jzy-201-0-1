package channel

import (
	"math"
	"testing"
)

func TestCorrelationIsAboveIndependentAndExample(t *testing.T) {
	ps := []float64{0.01, 0.01, 0.01}
	c := Compare(ps, .5)
	wantInd := 100 * (1 - math.Pow(.9999, 3))
	if math.Abs(c.IndependentOutagePct-wantInd) > 1e-12 {
		t.Fatalf("independent outage=%v want %v", c.IndependentOutagePct, wantInd)
	}
	if c.CorrelatedAvailability < c.IndependentAvailability {
		t.Fatalf("positive correlation availability %.9f below independent %.9f", c.CorrelatedAvailability, c.IndependentAvailability)
	}
	if c.DifferencePct < 0 {
		t.Fatalf("difference should show correlated has less outage, got %v", c.DifferencePct)
	}
	// With rho=1 all three identical events coincide; outage is one hop's outage.
	full := Compare(ps, .999)
	if math.Abs(full.CorrelatedOutagePct-.01) > 0.002 {
		t.Fatalf("near-full correlation outage=%.5f, want ~0.01", full.CorrelatedOutagePct)
	}
}
