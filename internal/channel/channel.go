package channel

import "math"

// IndependentAvailability multiplies per-hop availabilities. It assumes rain
// outages do not overlap, which gives the smallest union outage and is used as
// the conservative service-level planning target.
func IndependentAvailability(hopAvailability []float64) float64 {
	a := 1.0
	for _, x := range hopAvailability {
		a *= x
	}
	return a
}

// CorrelatedAvailability models adjacent hops as a first-order stationary
// Gaussian copula. Non-adjacent dependence propagates through the Markov chain.
// hopPPercent is each hop's annual rain-outage percentage and rho is adjacent
// correlation in Gaussian latent variables.
func CorrelatedAvailability(hopPPercent []float64, rho float64) float64 {
	if len(hopPPercent) == 0 {
		return 1
	}
	rho = math.Max(-0.999, math.Min(0.999, rho))
	qPrev := hopPPercent[0] / 100
	hPrev := normInv(1 - qPrev)
	allOK := 1 - qPrev
	for i := 1; i < len(hopPPercent); i++ {
		qI := hopPPercent[i] / 100
		hI := normInv(1 - qI)
		// Both adjacent hops exceed their rain margins. Outage is the upper tail
		// of latent variables; by symmetry it equals the lower-tail CDF at -h.
		bothOut := bivariateNormalCDF(-hPrev, -hI, rho)
		bothOK := 1 - qPrev - qI + bothOut
		allOK *= bothOK / (1 - qPrev)
		qPrev, hPrev = qI, hI
	}
	return allOK
}

type Comparison struct {
	IndependentAvailability float64   `json:"independent_availability"`
	CorrelatedAvailability  float64   `json:"correlated_availability"`
	IndependentOutagePct    float64   `json:"independent_outage_pct"`
	CorrelatedOutagePct     float64   `json:"correlated_outage_pct"`
	DifferencePct           float64   `json:"difference_pct"`
	HopOutagePct            []float64 `json:"hop_outage_pct"`
	Rho                     float64   `json:"rho"`
}

func Compare(hopPPercent []float64, rho float64) Comparison {
	hopA := make([]float64, len(hopPPercent))
	for i, p := range hopPPercent {
		hopA[i] = 1 - p/100
	}
	ind := IndependentAvailability(hopA)
	corr := CorrelatedAvailability(hopPPercent, rho)
	return Comparison{
		IndependentAvailability: ind,
		CorrelatedAvailability:  corr,
		IndependentOutagePct:    100 * (1 - ind),
		CorrelatedOutagePct:     100 * (1 - corr),
		DifferencePct:           100 * (corr - ind),
		HopOutagePct:            append([]float64(nil), hopPPercent...),
		Rho:                     rho,
	}
}

func normPhi(x float64) float64 { return 0.5 * math.Erfc(-x/math.Sqrt2) }

// normInv is Peter Acklam's rational inverse-normal approximation.
func normInv(p float64) float64 {
	a := []float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := []float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := []float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := []float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}
	plow, phigh := 0.02425, 1-0.02425
	if p < plow {
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
	if p <= phigh {
		q := p - 0.5
		r := q * q
		return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q / (((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	}
	q := math.Sqrt(-2 * math.Log(1-p))
	return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
}

// bivariateNormalCDF computes P(X<=h,Y<=k) by integrating the conditional
// normal density over x; 2000 Simpson/trapezoid steps are ample here.
func bivariateNormalCDF(h, k, rho float64) float64 {
	f := func(x float64) float64 {
		z := (k - rho*x) / math.Sqrt(1-rho*rho)
		return normPhi(z) * math.Exp(-x*x/2)
	}
	const n = 2000
	a, b := -8.0, h
	width := (b - a) / n
	sum := 0.5 * (f(a) + f(b))
	for i := 1; i < n; i++ {
		sum += f(a + float64(i)*width)
	}
	return sum * width / math.Sqrt(2*math.Pi)
}
