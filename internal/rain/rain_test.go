package rain

import (
	"testing"

	"microwave-network/internal/domain"
)

func TestRainRateMonotonicAvailability(t *testing.T) {
	band := domain.RainBand{ID: "b", FMinMHz: 17000, FMaxMHz: 19000, K: .07, Alpha: 1.08}
	distance := 10.0
	margin := 25.0
	prev := 2.0
	for _, r := range []float64{10, 20, 30, 40, 60, 80, 100} {
		zone := domain.ClimateZone{RainRate01PctMMH: r}
		p := ExceedancePercent(zone, band, 18000, distance, margin)
		a := 1 - p/100
		if a > prev+1e-12 {
			t.Fatalf("rain %.0f availability %.9f rose above %.9f", r, a, prev)
		}
		prev = a
	}
}

func TestExceedanceBounded(t *testing.T) {
	zone := domain.ClimateZone{RainRate01PctMMH: 50}
	band := domain.RainBand{K: .07, Alpha: 1.08}
	if p := ExceedancePercent(zone, band, 18000, 10, 0); p != 1 {
		t.Fatalf("zero margin p=%v", p)
	}
	if p := ExceedancePercent(zone, band, 18000, 10, 100000); p != .001 {
		t.Fatalf("huge margin p=%v", p)
	}
}
