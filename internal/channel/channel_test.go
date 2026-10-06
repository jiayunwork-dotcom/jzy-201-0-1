package channel

import (
	"math"
	"testing"
)

func TestThreeHopCorrelationExample(t *testing.T) {
	// Each hop has rain unavailability p=0.01% = 1e-4 fraction. rho=0.6 means
	// adjacent outage indicators are positively dependent under the Markov
	// copula-style mixture.
	p := 0.0001
	got := Combine([]float64{p, p, p}, 0.6)

	independent := 1 - math.Pow(1-p, 3)
	if math.Abs(got.IndependentUnavailability-independent) > 1e-15 {
		t.Fatalf("independent result = %.12f, want %.12f", got.IndependentUnavailability, independent)
	}
	// First-order Markov union:
	// joint adjacent = (1-rho)p^2 + rho p
	// U3 = 3p - 2*joint = 3p - 2rho p - 2(1-rho)p^2
	joint := 0.4*p*p + 0.6*p
	wantCorr := 3*p - 2*joint
	if math.Abs(got.AdjacentCorrelatedUnavailability-wantCorr) > 1e-15 {
		t.Fatalf("correlated result = %.12f, want %.12f", got.AdjacentCorrelatedUnavailability, wantCorr)
	}
	if got.ConservativeUnavailability != 3*p {
		t.Fatalf("conservative bound = %.12f, want %.12f", got.ConservativeUnavailability, 3*p)
	}
	// In this example the independent calibre is more pessimistic than the
	// adjacent-correlation calibre by about 2e-6 percentage points. The
	// positive dependence aligns common rain outages, so their union is smaller
	// than independence would suggest. The selected planning result is the
	// conservative sum bound, not the optimistic physical point estimate.
	if got.SelectedAvailability != got.ConservativeAvailability {
		t.Fatal("selected availability must be conservative bound")
	}
	if got.AdjacentCorrelatedUnavailability >= got.IndependentUnavailability {
		t.Fatal("expected positive adjacent dependence to reduce union outage relative to independence")
	}
	if got.IndependentUnavailability >= got.ConservativeUnavailability {
		t.Fatal("independent product cannot exceed sum upper bound")
	}
}

func TestRhoZeroAndOneBounds(t *testing.T) {
	p := 0.001
	r0 := Combine([]float64{p, p}, 0)
	if math.Abs(r0.AdjacentCorrelatedUnavailability-(2*p-p*p)) > 1e-15 {
		t.Fatalf("rho=0 should be independent, got %.12f", r0.AdjacentCorrelatedUnavailability)
	}
	r1 := Combine([]float64{p, p}, 1)
	if math.Abs(r1.AdjacentCorrelatedUnavailability-p) > 1e-15 {
		t.Fatalf("rho=1 perfectly comonotone equal p should be p, got %.12f", r1.AdjacentCorrelatedUnavailability)
	}
}
