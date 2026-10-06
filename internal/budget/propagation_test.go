package budget

import (
	"math"
	"testing"
)

func TestFreeSpaceLossReferenceAndDoubling(t *testing.T) {
	got := FreeSpaceLoss(18000, 10)
	if math.Abs(got-137.55) > 0.01 {
		t.Fatalf("FSPL = %.4f, want 137.55", got)
	}
	if d := FreeSpaceLoss(18000, 20) - got; math.Abs(d-6.0206) > 0.001 {
		t.Fatalf("doubling distance adds %.4f, want 6.02", d)
	}
	if d := FreeSpaceLoss(36000, 10) - got; math.Abs(d-6.0206) > 0.001 {
		t.Fatalf("doubling frequency adds %.4f, want 6.02", d)
	}
}
