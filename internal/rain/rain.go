// Package rain computes rain attenuation and rain unavailability for a
// line-of-sight microwave hop.
package rain

import (
	"math"

	"microwaveplanner/internal/linkbudget"
)

// Climate gives the rainfall statistics for one ITU-like rain climatic region.
// R001 is the 1-minute rainfall rate in mm/h exceeded for 0.01% of an average
// year (ITU-R P.837). Additional statistics can be stored in Rates.
type Climate struct {
	ID    string             `json:"id"`
	Name  string             `json:"name"`
	R001  float64            `json:"r001MMh"`
	Rates map[string]float64 `json:"rates,omitempty"`
}

// Coefficients are ITU-R P.838-3 specific-attenuation coefficients for one
// frequency band and polarization: gamma_R = k * R^alpha (dB/km).
type Coefficients struct {
	BandID       string                  `json:"bandId"`
	FrequencyGHz float64                 `json:"frequencyGHz"`
	Polarization linkbudget.Polarization `json:"polarization"`
	K            float64                 `json:"k"`
	Alpha        float64                 `json:"alpha"`
}

// Hop describes the rain path.
type Hop struct {
	DistanceKM   float64
	FrequencyGHz float64
	R001         float64
	K            float64
	Alpha        float64
}

// Method names the standard used by an answer.
const (
	MethodSpecificAttenuation = "ITU-R P.838-3, gamma_R = k R^alpha"
	MethodScaling             = "ITU-R P.530-17 §2.2.4 rain attenuation scaling (0.001%-1%)"
	MethodPathReduction       = "ITU-R P.530-17 §2.2.4.1 effective path length"
)

// SpecificAttenuationDBPerKM returns gamma_R in dB/km.
func SpecificAttenuationDBPerKM(rateMMH, k, alpha float64) float64 {
	return k * math.Pow(rateMMH, alpha)
}

// EffectivePathKM returns d_eff = d/(1+d/d0), d0 = 35 exp(-0.015 R001).
// This is the ITU-R P.530-17 path-reduction model for horizontal paths.
func EffectivePathKM(distanceKM, r001 float64) float64 {
	d0 := 35.0 * math.Exp(-0.015*r001)
	return distanceKM / (1.0 + distanceKM/d0)
}

// c0 implements C0 = 0.12 + 0.4 [log10(f/10)]^1.22.
func c0(frequencyGHz float64) float64 {
	if frequencyGHz < 10 {
		return 0.12
	}
	x := math.Log10(frequencyGHz / 10.0)
	return 0.12 + 0.4*math.Pow(x, 1.22)
}

type scalingConstants struct{ C0, C1, C2, C3 float64 }

func scaling(frequencyGHz float64) scalingConstants {
	c0v := c0(frequencyGHz)
	return scalingConstants{
		C0: c0v,
		// Formulae from ITU-R P.530-17, equations (10)-(13).
		C1: math.Pow(0.07, c0v) * math.Pow(0.12, 1.0-c0v),
		C2: 0.855*c0v + 0.546*(1.0-c0v),
		C3: 0.139*c0v + 0.043*(1.0-c0v),
	}
}

// A001 returns rain attenuation exceeded for 0.01% of an average year.
func A001(h Hop) float64 {
	gamma := SpecificAttenuationDBPerKM(h.R001, h.K, h.Alpha)
	return gamma * EffectivePathKM(h.DistanceKM, h.R001)
}

// attenuationFactor returns A_p / A_0.01 for exceedance percentage p.
func attenuationFactor(c scalingConstants, pPercent float64) float64 {
	// A_p/A001 = C1 * p^-(C2+C3 log10 p), p in percent.
	exponent := -(c.C2 + c.C3*math.Log10(pPercent))
	return c.C1 * math.Pow(pPercent, exponent)
}

// AttenuationDB returns attenuation exceeded for pPercent percent of a year.
// The standard scaling is valid for 0.001% <= p <= 1%; inputs are clamped to
// that interval so callers always receive a finite planning value.
func AttenuationDB(h Hop, pPercent float64) float64 {
	p := pPercent
	if p < 0.001 {
		p = 0.001
	}
	if p > 1 {
		p = 1
	}
	return A001(h) * attenuationFactor(scaling(h.FrequencyGHz), p)
}

// UnavailabilityPercent solves AttenuationDB(p)=fadeMargin and returns p in
// percent of an average year. Therefore availability fraction is
// 1 - p/100. The returned boolean is false when the requested margin lies
// outside the standard 0.001%-1% scaling interval; the numerical answer is
// then conservatively clamped at that interval boundary.
func UnavailabilityPercent(h Hop, fadeMarginDB float64) (float64, bool) {
	if fadeMarginDB <= 0 {
		return 100.0, false
	}
	a001 := A001(h)
	if a001 <= 0 || h.DistanceKM <= 0 || h.R001 <= 0 {
		return 0, true
	}
	c := scaling(h.FrequencyGHz)
	atMin := a001 * attenuationFactor(c, 0.001)
	atMax := a001 * attenuationFactor(c, 1.0)

	if fadeMarginDB >= atMin {
		// Outage is rarer than the 0.001% lower endpoint. Clamping at 0.001%
		// deliberately overstates unavailability for planning.
		return 0.001, false
	}
	if fadeMarginDB <= atMax {
		// Outage is more frequent than/near the 1% upper endpoint.
		return 1.0, false
	}

	// A(p) decreases monotonically in p over the valid interval. Bisection is
	// deterministic and avoids derivatives of the log-power expression.
	lo, hi := 0.001, 1.0
	for i := 0; i < 80; i++ {
		mid := (lo + hi) / 2
		if a001*attenuationFactor(c, mid) > fadeMarginDB {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2, true
}

// AvailabilityFraction is 1 - unavailabilityPercent/100.
func AvailabilityFraction(h Hop, fadeMarginDB float64) (float64, bool) {
	p, ok := UnavailabilityPercent(h, fadeMarginDB)
	return 1 - p/100, ok
}
