package linkbudget

import (
	"math"
	"testing"

	"microwaveplanner/internal/params"
)

func TestFreeSpaceReferenceAndDoubling(t *testing.T) {
	got := FreeSpaceLossDB(10, 18)
	if math.Abs(got-137.55) > 0.01 {
		t.Fatalf("18 GHz / 10 km FSPL = %.4f, want 137.55", got)
	}
	tests := []struct {
		name      string
		distance  float64
		frequency float64
	}{
		{"distance doubles", 20, 18},
		{"frequency doubles", 10, 36},
		{"both double", 20, 36},
	}
	base := FreeSpaceLossDB(10, 18)
	for _, tt := range tests {
		got := FreeSpaceLossDB(tt.distance, tt.frequency)
		want := base + 6.02
		if tt.name == "both double" {
			want = base + 2*6.02
		}
		if math.Abs(got-want) > 0.002 {
			t.Fatalf("%s: got %.4f, want %.4f", tt.name, got, want)
		}
	}
}

func endpoint(id string, tx, threshold, minF, maxF, lossPerM, feeder, splitter, extra, gain float64) *params.EffectiveEndpoint {
	radio := params.MergeFields(map[params.Layer]*params.VersionedSet{
		params.LayerNominal: {
			Ref: params.Ref{Layer: params.LayerNominal, Version: 1},
			Fields: map[string]float64{
				params.FieldRadioTxPowerDBm:      tx,
				params.FieldRadioRxThresholdDBm:  threshold,
				params.FieldRadioMinFrequencyGHz: minF,
				params.FieldRadioMaxFrequencyGHz: maxF,
				params.FieldRadioFeederLossDBpm:  lossPerM,
				params.FieldRadioSplitterLossDB:  splitter,
			},
		},
		params.LayerSite: {
			Ref: params.Ref{Layer: params.LayerSite, Version: 2},
			Fields: map[string]float64{
				params.FieldInstallationFeederM: feeder,
				params.FieldInstallationExtraDB: extra,
			},
		},
	})
	antenna := params.MergeFields(map[params.Layer]*params.VersionedSet{
		params.LayerNominal: {
			Ref:    params.Ref{Layer: params.LayerNominal, Version: 1},
			Fields: map[string]float64{params.FieldAntennaGainDBD: gain},
		},
	})
	return &params.EffectiveEndpoint{EndpointID: id, Radio: radio, Antenna: antenna}
}

func TestSwapEndsDoesNotChangeBudget(t *testing.T) {
	a := endpoint("a", 18, -72, 17, 19, 0.1, 20, 1, 0.5, 35)
	b := endpoint("b", 20, -70, 17, 19, 0.12, 30, 0.5, 0, 36)
	first, err := Evaluate(HopInput{LinkID: "l", EndA: a, EndB: b, FrequencyGHz: 18, DistanceKM: 10})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Evaluate(HopInput{LinkID: "l", EndA: b, EndB: a, FrequencyGHz: 18, DistanceKM: 10})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(first.FadeMarginDB-second.FadeMarginDB) > 1e-12 {
		t.Fatalf("swap changed limiting margin: %.12f vs %.12f", first.FadeMarginDB, second.FadeMarginDB)
	}
	if math.Abs(first.ReceivedLevelDBm-second.ReceivedLevelDBm) > 1e-12 {
		t.Fatalf("swap changed limiting receive level: %v vs %v", first.ReceivedLevelDBm, second.ReceivedLevelDBm)
	}
}

func TestFrequencyOutsideBandAndDistanceAndFinite(t *testing.T) {
	a := endpoint("a", 18, -70, 17, 19, 0.1, 0, 0, 0, 35)
	b := endpoint("b", 18, -70, 17, 19, 0.1, 0, 0, 0, 35)
	if _, err := Evaluate(HopInput{EndA: a, EndB: b, FrequencyGHz: 20, DistanceKM: 10}); err == nil {
		t.Fatal("expected unsupported frequency error")
	}
	_, err := Evaluate(HopInput{EndA: a, EndB: b, FrequencyGHz: 18, DistanceKM: 0})
	if err == nil {
		t.Fatal("expected non-positive distance error")
	}
	v := errAs(t, err)
	if !fieldExists(v, "distanceKM") {
		t.Fatalf("distance field missing: %v", err)
	}
}

func TestThresholdAboveMaxIsWarning(t *testing.T) {
	// tx 10 dBm plus two 10 dBi antennas with no local losses gives a
	// theoretical maximum receive level of 30 dBm, before free-space loss. A
	// threshold of 35 dBm is above even that and must be a warning.
	a := endpoint("a", 10, 35, 17, 19, 0.1, 0, 0, 0, 10)
	b := endpoint("b", 10, 35, 17, 19, 0.1, 0, 0, 0, 10)
	got, err := Evaluate(HopInput{EndA: a, EndB: b, FrequencyGHz: 18, DistanceKM: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("expected threshold warning")
	}
	if got.FadeMarginDB >= 0 {
		t.Fatalf("impossible threshold should yield negative margin, got %.2f", got.FadeMarginDB)
	}
}

func errAs(t *testing.T, err error) *ValidationErrors {
	t.Helper()
	var v *ValidationErrors
	if !asValidation(err, &v) {
		t.Fatalf("not validation error: %v", err)
	}
	return v
}

func asValidation(err error, target **ValidationErrors) bool {
	if v, ok := err.(*ValidationErrors); ok {
		*target = v
		return true
	}
	return false
}

func fieldExists(v *ValidationErrors, field string) bool {
	for _, e := range v.Errors {
		if e.Field == field {
			return true
		}
	}
	return false
}
