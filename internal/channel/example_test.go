package channel

import (
	"math"
	"testing"
)

// TestTwoCalibreUnequalHopWorkedExample fixes the documented unequal-hop
// comparison between independent multiplication, adjacent correlation and the
// conservative planning bound.
func TestTwoCalibreUnequalHopWorkedExample(t *testing.T) {
	// Two adjacent hops with different rain unavailability and rho=0.6.
	p1 := 0.001 // 0.1% of a year
	p2 := 0.0002
	got := Combine([]float64{p1, p2}, 0.6)

	wantIndependent := p1 + p2 - p1*p2
	if math.Abs(got.IndependentUnavailability-wantIndependent) > 1e-15 {
		t.Fatalf("independent = %.12f, want %.12f", got.IndependentUnavailability, wantIndependent)
	}
	wantJoint := 0.4*p1*p2 + 0.6*math.Min(p1, p2) // 0.4*p1*p2 + 0.6*p2
	wantCorrelated := p1 + p2 - wantJoint
	if math.Abs(got.AdjacentCorrelatedUnavailability-wantCorrelated) > 1e-15 {
		t.Fatalf("correlated = %.12f, want %.12f", got.AdjacentCorrelatedUnavailability, wantCorrelated)
	}
	if math.Abs(got.ConservativeUnavailability-(p1+p2)) > 1e-15 {
		t.Fatalf("conservative = %.12f, want %.12f", got.ConservativeUnavailability, p1+p2)
	}
	// Ordering for a union under positive dependence:
	// correlated physical estimate < independent product <= conservative bound.
	if !(got.AdjacentCorrelatedUnavailability < got.IndependentUnavailability &&
		got.IndependentUnavailability <= got.ConservativeUnavailability) {
		t.Fatalf("calibre ordering wrong: corr=%g indep=%g cons=%g",
			got.AdjacentCorrelatedUnavailability, got.IndependentUnavailability, got.ConservativeUnavailability)
	}
	if got.SelectedAvailability != got.ConservativeAvailability {
		t.Fatal("planning selection must be the conservative bound")
	}
	// Document the overestimate magnitudes.
	indepOverCorr := got.IndependentUnavailability - got.AdjacentCorrelatedUnavailability
	consOverCorr := got.ConservativeUnavailability - got.AdjacentCorrelatedUnavailability
	if math.Abs(indepOverCorr-(wantIndependent-wantCorrelated)) > 1e-15 {
		t.Fatalf("independent-vs-correlated gap = %.12g", indepOverCorr)
	}
	if consOverCorr <= indepOverCorr {
		t.Fatalf("conservative buffer %.12g must exceed independent overestimate %.12g",
			consOverCorr, indepOverCorr)
	}
}
