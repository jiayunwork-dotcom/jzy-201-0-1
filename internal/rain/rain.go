package rain

import (
	"math"

	"microwave-network/internal/domain"
)

const (
	minExceedancePercent = 0.001
	maxExceedancePercent = 1.0
)

// Attenuation computes terrestrial line-of-sight rain attenuation exceeded for
// p% of an average year, using the ITU-R P.530 step-by-step rain procedure:
// P.838 gives gamma_R = k*R0.01^alpha; P.530 gives effective path length and
// the 0.001%-1% power-law conversion.
func Attenuation(zone domain.ClimateZone, band domain.RainBand, frequencyMHz, distanceKm, exceedancePercent float64) float64 {
	if distanceKm <= 0 || zone.RainRate01PctMMH <= 0 {
		return 0
	}
	fGHz := math.Max(frequencyMHz/1000, 0.001)
	gamma := band.K * math.Pow(zone.RainRate01PctMMH, band.Alpha)

	// P.530 equation 32 horizontal reduction factor. Guard the rare very-short
	// path where the fitted denominator is not positive by using the raw path.
	denominator := 0.477*math.Pow(distanceKm, 0.633)*
		math.Pow(zone.RainRate01PctMMH, 0.073*band.Alpha)*
		math.Pow(fGHz, 0.123) -
		10.579*(1-math.Exp(-0.024*distanceKm))
	r := 1.0
	if denominator > 0 {
		r = math.Min(1/denominator, 2.5)
	}
	effectiveDistanceKm := r * distanceKm
	a01 := gamma * effectiveDistanceKm

	p := math.Max(minExceedancePercent, math.Min(maxExceedancePercent, exceedancePercent))
	c0 := 0.12
	if fGHz >= 10 {
		c0 += 0.4 * math.Pow(math.Log10(fGHz/10), 0.8)
	}
	c1 := math.Pow(0.07, c0) * math.Pow(0.12, 1-c0)
	c2 := 0.855*c0 + 0.546*(1-c0)
	c3 := 0.139*c0 + 0.043*(1-c0)
	return a01 * c1 * math.Pow(p, -(c2+c3*math.Log10(p)))
}

// ExceedancePercent inverts Attenuation by bisection. The returned p is the
// percentage of an average year that rain fade exceeds the supplied margin.
func ExceedancePercent(zone domain.ClimateZone, band domain.RainBand, frequencyMHz, distanceKm, marginDB float64) float64 {
	if marginDB <= 0 {
		return maxExceedancePercent
	}
	lo, hi := minExceedancePercent, maxExceedancePercent
	aLo := Attenuation(zone, band, frequencyMHz, distanceKm, lo)
	if marginDB >= aLo {
		return minExceedancePercent
	}
	aHi := Attenuation(zone, band, frequencyMHz, distanceKm, hi)
	if marginDB <= aHi {
		return maxExceedancePercent
	}
	for i := 0; i < 80; i++ {
		mid := (lo + hi) / 2
		a := Attenuation(zone, band, frequencyMHz, distanceKm, mid)
		if a > marginDB {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

func Availability(zone domain.ClimateZone, band domain.RainBand, frequencyMHz, distanceKm, marginDB float64) float64 {
	return 1 - ExceedancePercent(zone, band, frequencyMHz, distanceKm, marginDB)/100
}
