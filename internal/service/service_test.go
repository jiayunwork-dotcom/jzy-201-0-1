package service

import (
	"context"
	"reflect"
	"testing"

	"microwave-network/internal/domain"
	"microwave-network/internal/params"
	"microwave-network/internal/store"
	"microwave-network/internal/task"
)

func seedApp(t *testing.T) (*App, domain.Link, domain.Link) {
	t.Helper()
	st := store.NewMemory()
	app := NewApp(st)
	ctx := context.Background()
	_, _ = app.Params.SaveRadio(ctx, domain.RadioModel{ID: "r-used", Name: "used", FMinMHz: 17000, FMaxMHz: 19000})
	_, _ = app.Params.SaveRadio(ctx, domain.RadioModel{ID: "r-other", Name: "other", FMinMHz: 17000, FMaxMHz: 19000})
	_, _ = app.Params.SaveAntenna(ctx, domain.AntennaModel{ID: "a1", Name: "ant"})
	_, _ = app.Params.SaveSite(ctx, domain.Site{ID: "s1", Name: "s1"})
	_, _ = app.Params.SaveSite(ctx, domain.Site{ID: "s2", Name: "s2"})
	_, _ = app.Params.SaveSite(ctx, domain.Site{ID: "s3", Name: "s3"})
	_, _ = app.Params.SaveSite(ctx, domain.Site{ID: "s4", Name: "s4"})
	_, _ = app.Params.SaveBatch(ctx, domain.Batch{ID: "b1", Name: "b1"})
	_, _ = app.Params.SaveClimate(ctx, domain.ClimateZone{ID: "z1", Name: "z", RainRate01PctMMH: 50})
	_, _ = app.Params.SaveRainBand(ctx, domain.RainBand{ID: "rb1", Name: "18", FMinMHz: 17000, FMaxMHz: 19000, K: .07, Alpha: 1.08})
	publishS(t, app, domain.LayerRadioModel, "r-used", map[string]float64{
		domain.FieldTxPower: 20, domain.FieldRxThreshold: -75, domain.FieldFeederLossPerM: .1, domain.FieldFeederLength: 10,
	})
	publishS(t, app, domain.LayerRadioModel, "r-other", map[string]float64{
		domain.FieldTxPower: 20, domain.FieldRxThreshold: -75, domain.FieldFeederLossPerM: .1, domain.FieldFeederLength: 10,
	})
	publishS(t, app, domain.LayerBatch, "b1", map[string]float64{domain.FieldTxPower: 20})
	publishS(t, app, domain.LayerAntenna, "a1", map[string]float64{domain.FieldAntennaGain: 38})

	used := domain.Link{ID: "l-used", Name: "used", DistanceKm: 10, ClimateZoneID: "z1", RainBandID: "rb1",
		A: domain.Endpoint{SiteID: "s1", RadioModelID: "r-used", AntennaModelID: "a1", FrequencyMHz: 18000},
		B: domain.Endpoint{SiteID: "s2", RadioModelID: "r-used", AntennaModelID: "a1", FrequencyMHz: 18000},
	}
	other := domain.Link{ID: "l-other", Name: "other", DistanceKm: 10, ClimateZoneID: "z1", RainBandID: "rb1",
		A: domain.Endpoint{SiteID: "s3", RadioModelID: "r-other", AntennaModelID: "a1", FrequencyMHz: 18000},
		B: domain.Endpoint{SiteID: "s2", RadioModelID: "r-other", AntennaModelID: "a1", FrequencyMHz: 18000},
	}
	disjoint := domain.Link{ID: "l-far", Name: "far", DistanceKm: 10, ClimateZoneID: "z1", RainBandID: "rb1",
		A: domain.Endpoint{SiteID: "s4", RadioModelID: "r-used", AntennaModelID: "a1", FrequencyMHz: 18000},
		B: domain.Endpoint{SiteID: "s3", RadioModelID: "r-used", AntennaModelID: "a1", FrequencyMHz: 18000},
	}
	if _, err := app.Topology.SaveLink(ctx, used); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Topology.SaveLink(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Topology.SaveLink(ctx, disjoint); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Topology.SaveChannel(ctx, domain.Channel{ID: "c1", Name: "c", LinkIDs: []string{"l-used"}, TargetAvailability: .999}); err != nil {
		t.Fatal(err)
	}
	return app, used, other
}

func mustS(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func okS[T any](t *testing.T, v T, err error) T {
	t.Helper()
	mustS(t, err)
	return v
}

func publishS(t *testing.T, app *App, layer, scope string, fields map[string]float64) {
	t.Helper()
	_, err := app.Params.PatchDraft(context.Background(), params.DraftPatch{Layer: layer, ScopeID: scope, Upserts: fields})
	mustS(t, err)
	_, _, err = app.PublishAndEnqueue(context.Background(), params.PublishRequest{Layer: layer, ScopeID: scope})
	mustS(t, err)
	_, err = app.Tasks.RunAvailable(context.Background())
	mustS(t, err)
}

func TestAffectedListExactAndComparison(t *testing.T) {
	app, used, other := seedApp(t)
	ctx := context.Background()
	refs, _ := app.Params.LatestRefs(ctx)
	beforeUsed, _ := app.Evaluator.EvaluateLink(ctx, used, refs)
	beforeOther, _ := app.Evaluator.EvaluateLink(ctx, other, refs)
	_, err := app.Params.PatchDraft(ctx, params.DraftPatch{Layer: domain.LayerRadioModel, ScopeID: "r-other", Upserts: map[string]float64{domain.FieldTxPower: 19}})
	if err != nil {
		t.Fatal(err)
	}
	_, job, err := app.PublishAndEnqueue(ctx, params.PublishRequest{Layer: domain.LayerRadioModel, ScopeID: "r-other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Tasks.RunAvailable(ctx); err != nil {
		t.Fatal(err)
	}
	done, err := app.Tasks.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != task.StatusComplete {
		t.Fatalf("status=%s err=%s", done.Status, done.ErrorMessage)
	}
	if !reflect.DeepEqual(done.Result.LinksAssessed, []string{"l-other"}) {
		t.Fatalf("affected links=%v want [l-other]", done.Result.LinksAssessed)
	}
	latestRefs, _ := app.Params.LatestRefs(ctx)
	afterUsed, _ := app.Evaluator.EvaluateLink(ctx, used, latestRefs)
	if afterUsed.Budget.WorstFadeMarginDB != beforeUsed.Budget.WorstFadeMarginDB {
		t.Fatal("unrelated link result changed")
	}
	afterOther, _ := app.Evaluator.EvaluateLink(ctx, other, done.StartRefs)
	if afterOther.Budget.WorstFadeMarginDB >= beforeOther.Budget.WorstFadeMarginDB {
		t.Fatal("reduced Tx power did not reduce margin")
	}
	if len(done.Result.MarginsReduced) != 1 || done.Result.MarginsReduced[0].LinkID != "l-other" {
		t.Fatalf("margin comparison=%+v", done.Result.MarginsReduced)
	}
}

func TestVersionPublishedDuringRunningJobFollowup(t *testing.T) {
	app, _, _ := seedApp(t)
	ctx := context.Background()
	patch := func(scope string, p float64) {
		_, err := app.Params.PatchDraft(ctx, params.DraftPatch{Layer: domain.LayerRadioModel, ScopeID: scope, Upserts: map[string]float64{domain.FieldTxPower: p}})
		if err != nil {
			t.Fatal(err)
		}
	}
	patch("r-used", 19)
	_, j1, err := app.PublishAndEnqueue(ctx, params.PublishRequest{Layer: domain.LayerRadioModel, ScopeID: "r-used"})
	if err != nil {
		t.Fatal(err)
	}
	patch("r-used", 18)
	_, j2, err := app.PublishAndEnqueue(ctx, params.PublishRequest{Layer: domain.LayerRadioModel, ScopeID: "r-used"})
	if err != nil {
		t.Fatal(err)
	}
	if refFor(j1.StartRefs, "r-used") == refFor(j2.StartRefs, "r-used") {
		t.Fatal("second job did not pin a different version")
	}
	ran, err := app.Tasks.RunAvailable(ctx)
	if err != nil || !ran {
		t.Fatalf("run j1 ran=%v err=%v", ran, err)
	}
	done1, _ := app.Tasks.Get(ctx, j1.ID)
	if done1.Status != task.StatusComplete || done1.StartRefs == nil {
		t.Fatalf("j1 wrong: %+v", done1)
	}
	ran, _ = app.Tasks.RunAvailable(ctx)
	if !ran {
		t.Fatal("second publication did not retain an executable job")
	}
	done2, _ := app.Tasks.Get(ctx, j2.ID)
	if done2.Status != task.StatusComplete {
		t.Fatalf("j2 status=%s err=%s", done2.Status, done2.ErrorMessage)
	}
}

func refFor(refs []domain.VersionRef, scope string) string {
	for _, r := range refs {
		if r.Layer == domain.LayerRadioModel && r.ScopeID == scope {
			return r.Version
		}
	}
	return ""
}

func TestHistoryReplay(t *testing.T) {
	app, used, _ := seedApp(t)
	ctx := context.Background()
	refs, _ := app.Params.LatestRefs(ctx)
	rec, err := app.Evaluator.EvaluateAndSaveLink(ctx, used.ID, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.VersionRefs) == 0 {
		t.Fatal("assessment missing provenance")
	}
	replayed, err := app.Evaluator.Replay(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Link.Budget.WorstFadeMarginDB != rec.Link.Budget.WorstFadeMarginDB {
		t.Fatal("replay mismatch")
	}
}

func TestChannelAdjacencyAndMissingReferencesValidation(t *testing.T) {
	app, _, _ := seedApp(t)
	ctx := context.Background()
	_, errInvalid := app.Topology.SaveChannel(ctx, domain.Channel{ID: "bad", Name: "bad", TargetAvailability: .999, LinkIDs: []string{"l-used", "l-far"}})
	if errInvalid == nil {
		t.Fatal("channel with non-adjacent links was accepted")
	}
	badLink := domain.Link{ID: "bad-link", Name: "bad", DistanceKm: 10, ClimateZoneID: "z1", RainBandID: "rb1",
		A: domain.Endpoint{SiteID: "s1", RadioModelID: "missing", AntennaModelID: "a1", FrequencyMHz: 18000},
		B: domain.Endpoint{SiteID: "s2", RadioModelID: "r-used", AntennaModelID: "a1", FrequencyMHz: 18000},
	}
	if _, err := app.Topology.SaveLink(ctx, badLink); err == nil {
		t.Fatal("missing model reference accepted")
	}
	badLink.A.RadioModelID = "r-used"
	badLink.A.FrequencyMHz = 20000
	if _, err := app.Topology.SaveLink(ctx, badLink); err == nil {
		t.Fatal("unsupported frequency accepted")
	}
	badLink.A.FrequencyMHz = 18000
	badLink.DistanceKm = 0
	if _, err := app.Topology.SaveLink(ctx, badLink); err == nil {
		t.Fatal("non-positive distance accepted")
	}
}
