package budget

import (
	"math"
	"testing"

	"microwave-network/internal/domain"
	"microwave-network/internal/params"
)

func eff(txPower, threshold, loss, gain float64) params.EffectiveEndpoint {
	return params.EffectiveEndpoint{
		Values: map[string]float64{
			domain.EffectiveTxPower:     txPower,
			domain.EffectiveRxThreshold: threshold,
			domain.EffectiveTotalLoss:   loss,
			domain.EffectiveAntennaGain: gain,
		},
	}
}

func TestEndpointSwapSymmetric(t *testing.T) {
	l := domain.Link{ID: "l", DistanceKm: 10, A: domain.Endpoint{SiteID: "s1", FrequencyMHz: 18000}, B: domain.Endpoint{SiteID: "s2", FrequencyMHz: 18000}}
	a := eff(20, -75, 2, 38)
	b := eff(22, -74, 3, 39)
	r1, _ := Evaluate(l, a, b)
	l.A, l.B = l.B, l.A
	r2, _ := Evaluate(l, b, a)
	if math.Abs(r1.WorstFadeMarginDB-r2.WorstFadeMarginDB) > 1e-12 {
		t.Fatal("worst margin changed on swap")
	}
	if math.Abs(r1.DirectionAToB.FadeMarginDB-r2.DirectionBToA.FadeMarginDB) > 1e-12 {
		t.Fatal("directional budget not swapped exactly")
	}
}

func TestThresholdAbovePhysicalMaximumWarns(t *testing.T) {
	l := domain.Link{DistanceKm: 10, A: domain.Endpoint{FrequencyMHz: 18000}, B: domain.Endpoint{FrequencyMHz: 18000}}
	r, _ := Evaluate(l, eff(20, 100, 2, 38), eff(22, -74, 3, 39))
	if len(r.Warnings) == 0 {
		t.Fatal("expected threshold warning, got none")
	}
}
