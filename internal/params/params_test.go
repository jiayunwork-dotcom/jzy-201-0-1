package params

import (
	"testing"
)

func TestMergeOrderIndependentAndSources(t *testing.T) {
	build := func() map[Layer]*VersionedSet {
		return map[Layer]*VersionedSet{
			LayerNominal: {
				Ref: Ref{Layer: LayerNominal, Version: 1}, SetID: "nom",
				Fields: map[string]float64{"tx": 20, "threshold": -70, "gain": 30},
			},
			LayerBatch: {
				Ref: Ref{Layer: LayerBatch, Version: 4}, SetID: "batch",
				Fields: map[string]float64{"tx": 21},
			},
			LayerSite: {
				Ref: Ref{Layer: LayerSite, Version: 9}, SetID: "site",
				Fields: map[string]float64{"gain": 31.5},
			},
		}
	}
	got := MergeFields(build())
	if got.Fields["tx"].Value != 21 || got.Fields["tx"].Source.Layer != LayerBatch || got.Fields["tx"].Source.Version != 4 {
		t.Fatalf("tx source wrong: %+v", got.Fields["tx"])
	}
	if got.Fields["gain"].Value != 31.5 || got.Fields["gain"].Source.Layer != LayerSite {
		t.Fatalf("gain source wrong: %+v", got.Fields["gain"])
	}
	if got.Fields["threshold"].Value != -70 || got.Fields["threshold"].Source.Layer != LayerNominal {
		t.Fatalf("threshold should inherit nominal: %+v", got.Fields["threshold"])
	}
	if got.Hash() != MergeFields(build()).Hash() {
		t.Fatal("merge hash differs across independently entered but identical sets")
	}
}

func TestSiteDeleteFallsBackToBatchThenNominal(t *testing.T) {
	nominal := &VersionedSet{Ref: Ref{Layer: LayerNominal, Version: 1}, Fields: map[string]float64{"x": 1}}
	batch := &VersionedSet{Ref: Ref{Layer: LayerBatch, Version: 2}, Fields: map[string]float64{"x": 2}}
	site := &VersionedSet{Ref: Ref{Layer: LayerSite, Version: 3}, Fields: map[string]float64{"x": 3}}

	merged := MergeFields(map[Layer]*VersionedSet{LayerNominal: nominal, LayerBatch: batch, LayerSite: site})
	if merged.Fields["x"].Value != 3 {
		t.Fatalf("site override expected, got %v", merged.Fields["x"])
	}
	merged = MergeFields(map[Layer]*VersionedSet{LayerNominal: nominal, LayerBatch: batch})
	if merged.Fields["x"].Value != 2 || merged.Fields["x"].Source.Layer != LayerBatch {
		t.Fatalf("deleted site field should fall back to batch: %+v", merged.Fields["x"])
	}
	merged = MergeFields(map[Layer]*VersionedSet{LayerNominal: nominal})
	if merged.Fields["x"].Value != 1 || merged.Fields["x"].Source.Layer != LayerNominal {
		t.Fatalf("deleted batch/site field should fall back to nominal: %+v", merged.Fields["x"])
	}
}
