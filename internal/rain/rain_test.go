package rain

import "testing"

func TestRainRateMonotonicAvailability(t *testing.T) {
	base := Hop{DistanceKM: 10, FrequencyGHz: 18, K: 0.07, Alpha: 1.08}
	margin := 40.0
	prev := 1.0
	for _, r := range []float64{10, 20, 30, 50, 70, 100} {
		h := base
		h.R001 = r
		p, _ := UnavailabilityPercent(h, margin)
		availability := 1 - p/100
		if availability > prev+1e-12 {
			t.Fatalf("availability rose as rain rate rose: R=%v A=%.8f previous=%.8f", r, availability, prev)
		}
		prev = availability
	}
}

func TestAttenuationAtLowerPercentIsHigher(t *testing.T) {
	h := Hop{DistanceKM: 10, FrequencyGHz: 18, R001: 50, K: 0.07, Alpha: 1.08}
	a001 := AttenuationDB(h, 0.01)
	a01 := AttenuationDB(h, 0.1)
	a1 := AttenuationDB(h, 1)
	if !(a001 > a01 && a01 > a1) {
		t.Fatalf("rain attenuation ordering wrong: A.01=%.3f A.1=%.3f A1=%.3f", a001, a01, a1)
	}
}
