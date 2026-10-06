// Package channel combines per-hop rain unavailability for tandem channels.
package channel

import (
	"math"
)

// Result contains three transparent reporting calibres plus the selected
// planning result.
type Result struct {
	HopCount int `json:"hopCount"`

	// IndependentAvailability assumes hops are mutually independent and
	// multiplies hop availabilities. It is a widely used simple lower bound.
	IndependentUnavailability float64 `json:"independentUnavailabilityFraction"`
	IndependentAvailability   float64 `json:"independentAvailability"`

	// AdjacentCorrelatedUnavailability uses a first-order Markov positive-
	// dependence model for physically adjacent hops. Non-adjacent hops remain
	// conditionally independent.
	AdjacentCorrelatedUnavailability float64 `json:"adjacentCorrelatedUnavailabilityFraction"`
	AdjacentCorrelatedAvailability   float64 `json:"adjacentCorrelatedAvailability"`

	// ConservativeUnavailability is sum(p_i), the Fréchet upper bound for a
	// union of events. It is the value used for the pass/fail target by
	// default.
	ConservativeUnavailability float64 `json:"conservativeUnavailabilityFraction"`
	ConservativeAvailability   float64 `json:"conservativeAvailability"`

	// Selected values default to the conservative planning calibre.
	SelectedUnavailability float64 `json:"selectedUnavailabilityFraction"`
	SelectedAvailability   float64 `json:"selectedAvailability"`
	Rho                    float64 `json:"rho"`
}

// Combine builds all calibres. hopP are per-hop rain unavailability fractions.
// rho is the correlation strength between outage indicators of physically
// adjacent hops, 0 <= rho <= 1.
//
// The adjacent joint outage used in the Markov model is
//
//	P(E_i ∩ E_{i+1}) = (1-rho)*p_i*p_{i+1} + rho*min(p_i,p_{i+1}).
//
// rho=0 reproduces independence; rho=1 is perfect positive rank dependence for
// the adjacent pair. The recurrence P(any up to i) is
//
//	U_i = U_{i-1} + p_i - P(E_i ∩ any_{i-1})
//
// with only the immediately preceding hop retained (first-order Markov):
//
//	P(E_i ∩ any_{i-1}) = P(E_i ∩ E_{i-1}).
func Combine(hopP []float64, rho float64) Result {
	r := Result{HopCount: len(hopP), Rho: clampRho(rho)}

	independentP := 0.0
	independentA := 1.0
	sumP := 0.0
	for _, p := range hopP {
		p = clamp01(p)
		independentP = independentP + p - independentP*p
		independentA *= 1 - p
		sumP += p
	}
	r.IndependentUnavailability = independentP
	r.IndependentAvailability = independentA
	r.ConservativeUnavailability = math.Min(1, sumP)
	r.ConservativeAvailability = 1 - r.ConservativeUnavailability

	if len(hopP) == 0 {
		r.AdjacentCorrelatedAvailability = 1
		r.SelectedAvailability = 1
		return r
	}

	u := clamp01(hopP[0])
	prevP := u
	for _, p0 := range hopP[1:] {
		p := clamp01(p0)
		joint := (1-r.Rho)*prevP*p + r.Rho*math.Min(prevP, p)
		u = u + p - joint
		prevP = p
	}
	u = clamp01(u)
	r.AdjacentCorrelatedUnavailability = u
	r.AdjacentCorrelatedAvailability = 1 - u

	r.SelectedUnavailability = r.ConservativeUnavailability
	r.SelectedAvailability = r.ConservativeAvailability
	return r
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}

func clampRho(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}
