package params

import (
	"context"
	"sort"
	"testing"

	"microwave-network/internal/domain"
	"microwave-network/internal/store"
)

func setupModel(t *testing.T) (*Service, store.Store) {
	t.Helper()
	st := store.NewMemory()
	ps := New(st)
	ctx := context.Background()
	_, _ = ps.SaveRadio(ctx, domain.RadioModel{ID: "r1", Name: "Radio", FMinMHz: 17000, FMaxMHz: 19000})
	_, _ = ps.SaveAntenna(ctx, domain.AntennaModel{ID: "a1", Name: "Ant"})
	_, _ = ps.SaveSite(ctx, domain.Site{ID: "s1", Name: "Site"})
	_, _ = ps.SaveBatch(ctx, domain.Batch{ID: "b1", Name: "Batch"})
	return ps, st
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func ok[T any](t *testing.T, v T, err error) T {
	t.Helper()
	must(t, err)
	return v
}

func publishFields(t *testing.T, ps *Service, layer, scope string, fields map[string]float64) domain.Version {
	t.Helper()
	d, err := ps.PatchDraft(context.Background(), DraftPatch{Layer: layer, ScopeID: scope, Upserts: fields})
	if err != nil {
		t.Fatal(err)
	}
	rev := d.Revision
	pr, err := ps.Publish(context.Background(), PublishRequest{Layer: layer, ScopeID: scope})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Version.Revision != rev {
		t.Fatalf("version pinned revision %d, draft revision %d", pr.Version.Revision, rev)
	}
	return pr.Version
}

func TestMergeOrderIndependentAndSources(t *testing.T) {
	ps, _ := setupModel(t)
	ctx := context.Background()
	// Add layers in arbitrary order (site, antenna, batch, nominal).
	publishFields(t, ps, domain.LayerSite, "s1", map[string]float64{domain.FieldFeederLength: 30})
	publishFields(t, ps, domain.LayerAntenna, "a1", map[string]float64{domain.FieldAntennaGain: 38})
	publishFields(t, ps, domain.LayerBatch, "b1", map[string]float64{domain.FieldTxPower: 21})
	publishFields(t, ps, domain.LayerRadioModel, "r1", map[string]float64{
		domain.FieldTxPower: 20, domain.FieldRxThreshold: -75, domain.FieldFeederLossPerM: .1,
	})
	refs, err := ps.LatestRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	refs = sorted(refs)
	// Reverse is semantically irrelevant because merge uses fixed precedence.
	rev := append([]domain.VersionRef(nil), refs...)
	sort.Slice(rev, func(i, j int) bool { return rev[i].Version > rev[j].Version })
	e := domain.Endpoint{SiteID: "s1", RadioModelID: "r1", AntennaModelID: "a1", BatchID: "b1", FrequencyMHz: 18000}
	a, err := ps.Effective(ctx, e, refs)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ps.Effective(ctx, e, rev)
	if err != nil {
		t.Fatal(err)
	}
	if a.Values[domain.EffectiveTxPower] != 21 {
		t.Fatalf("tx=%v", a.Values)
	}
	if src := a.Sources[domain.EffectiveTxPower]; src.Layer != domain.LayerBatch || src.Version == "" {
		t.Fatalf("source=%+v", src)
	}
	if a.Values[domain.EffectiveFeederLength] != 30 || a.Sources[domain.EffectiveFeederLength].Layer != domain.LayerSite {
		t.Fatalf("length source=%+v", a.Sources[domain.EffectiveFeederLength])
	}
	if a.Values[domain.EffectiveRxThreshold] != -75 || a.Sources[domain.EffectiveRxThreshold].Layer != domain.LayerRadioModel {
		t.Fatal("nominal inheritance failed")
	}
	if a.Values[domain.EffectiveTotalFeederLoss] != 3 {
		t.Fatalf("total feeder=%v", a.Values)
	}
	for k := range a.Values {
		if a.Values[k] != b.Values[k] {
			t.Fatalf("value %s order dependent: %v != %v", k, a.Values[k], b.Values[k])
		}
	}
}

func TestDeleteFieldFallsBack(t *testing.T) {
	ps, st := setupModel(t)
	ctx := context.Background()
	publishFields(t, ps, domain.LayerAntenna, "a1", map[string]float64{domain.FieldAntennaGain: 38})
	publishFields(t, ps, domain.LayerRadioModel, "r1", map[string]float64{
		domain.FieldTxPower: 20, domain.FieldRxThreshold: -75, domain.FieldFeederLength: 10, domain.FieldFeederLossPerM: .1,
	})
	publishFields(t, ps, domain.LayerSite, "s1", map[string]float64{domain.FieldFeederLength: 20})
	v2, err := ps.PatchDraft(ctx, DraftPatch{Layer: domain.LayerSite, ScopeID: "s1", Deletes: []string{domain.FieldFeederLength}})
	if err != nil {
		t.Fatal(err)
	}
	if v2.Fields[domain.FieldFeederLength] != 0 {
		t.Fatal("deleted field remains")
	}
	pr, err := ps.Publish(ctx, PublishRequest{Layer: domain.LayerSite, ScopeID: "s1"})
	if err == nil {
		t.Fatal("empty site publication unexpectedly allowed")
	}
	// Keep another site field so publication is legal; target field is absent and inherits nominal.
	if _, err := ps.PatchDraft(ctx, DraftPatch{Layer: domain.LayerSite, ScopeID: "s1", Upserts: map[string]float64{domain.FieldSplitterCount: 1}}); err != nil {
		t.Fatal(err)
	}
	pr, err = ps.Publish(ctx, PublishRequest{Layer: domain.LayerSite, ScopeID: "s1"})
	must(t, err)
	refs, _ := ps.LatestRefs(ctx)
	e := domain.Endpoint{SiteID: "s1", RadioModelID: "r1", AntennaModelID: "a1", FrequencyMHz: 18000}
	eff, err := EffectiveWithStore(ctx, st, e, refs)
	must(t, err)
	if eff.Values[domain.EffectiveFeederLength] != 10 || eff.Sources[domain.EffectiveFeederLength].Layer != domain.LayerRadioModel {
		t.Fatalf("fallback failed: %v source %+v", eff.Values, eff.Sources[domain.EffectiveFeederLength])
	}
	if pr.Version.Fields[domain.FieldFeederLength] != 0 {
		t.Fatal("published version contains deleted field")
	}
}

func TestDraftConflict(t *testing.T) {
	ps, _ := setupModel(t)
	ctx := context.Background()
	d1, err := ps.PatchDraft(ctx, DraftPatch{Layer: domain.LayerRadioModel, ScopeID: "r1", Upserts: map[string]float64{domain.FieldTxPower: 20}})
	must(t, err)
	if _, err := ps.PatchDraft(ctx, DraftPatch{Layer: domain.LayerRadioModel, ScopeID: "r1", Upserts: map[string]float64{domain.FieldTxPower: 21}, ExpectedRevision: &d1.Revision}); err != nil {
		t.Fatal(err)
	}
	stale := int64(1)
	if _, err := ps.PatchDraft(ctx, DraftPatch{Layer: domain.LayerRadioModel, ScopeID: "r1", Upserts: map[string]float64{domain.FieldTxPower: 22}, ExpectedRevision: &stale}); err != domain.ErrConflict {
		t.Fatalf("stale save err=%v, want conflict", err)
	}
}

func sorted(refs []domain.VersionRef) []domain.VersionRef { return store.VersionRefs(refs) }
