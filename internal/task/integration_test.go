package task

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"microwaveplanner/internal/assessment"
	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/model"
	"microwaveplanner/internal/params"
	"microwaveplanner/internal/store"
)

// testDB opens a disposable MySQL-compatible database. Set
// MW_TEST_MYSQL_DSN to run these integration tests; otherwise they skip.
func testDB(t *testing.T) *store.Store {
	t.Helper()
	dsn := getenvDefault("MW_TEST_MYSQL_DSN", "root@tcp(127.0.0.1:3306)/")
	name := fmt.Sprintf("mw_test_%d_%04d", time.Now().UnixNano(), rand.Intn(10000))
	st, err := store.NewWithDatabase(dsn, name)
	if err != nil {
		t.Skipf("integration database unavailable (%v); set MW_TEST_MYSQL_DSN to run", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func getenvDefault(key, fallback string) string {
	// Avoid importing os in the assertion helpers; this tiny wrapper keeps the
	// environment contract explicit.
	return envOr(key, fallback)
}

type integrationFixture struct {
	t           *testing.T
	ctx         context.Context
	st          *store.Store
	svc         *assessment.Service
	mgr         *Manager
	prefix      string
	radio       string
	antenna     string
	region      string
	coefficient string
	sites       [3]string
	ends        [4]string
	links       [2]string
	channel     string
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	ctx := context.Background()
	st := testDB(t)
	svc := assessment.New(st)
	mgr := NewManager(st, svc)
	p := fmt.Sprintf("it%d", time.Now().UnixNano())
	f := &integrationFixture{
		t: t, ctx: ctx, st: st, svc: svc, mgr: mgr, prefix: p,
		radio: p + "_radio", antenna: p + "_antenna", region: p + "_rain",
		coefficient: p + "_18ghz", channel: p + "_channel",
		sites: [3]string{p + "_s1", p + "_s2", p + "_s3"},
		ends:  [4]string{p + "_e1", p + "_e2", p + "_e3", p + "_e4"},
		links: [2]string{p + "_l1", p + "_l2"},
	}

	radioFields := map[string]float64{
		params.FieldRadioTxPowerDBm:      15,
		params.FieldRadioRxThresholdDBm:  -85,
		params.FieldRadioMinFrequencyGHz: 17.5,
		params.FieldRadioMaxFrequencyGHz: 18.7,
		params.FieldRadioFeederLossDBpm:  0.12,
		params.FieldRadioSplitterLossDB:  0,
	}
	antennaFields := map[string]float64{params.FieldAntennaGainDBD: 38}
	f.mustPublish(params.LayerNominal, "radio_model", f.radio, radioFields)
	f.mustPublish(params.LayerNominal, "antenna_model", f.antenna, antennaFields)

	if err := st.CreateRainClimate(ctx, model.RainClimate{ID: f.region, Name: f.region, R001: 30}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateRainBandCoefficient(ctx, model.RainBandCoefficient{
		ID: f.prefix + "_coef", BandID: f.coefficient, FrequencyGHz: 18.1,
		Polarization: linkbudget.PolarizationHorizontal, K: 0.07, Alpha: 1.08,
	}); err != nil {
		t.Fatal(err)
	}
	for i, id := range f.sites {
		if err := st.CreateSite(ctx, model.Site{ID: id, Name: id, Region: f.region}); err != nil {
			t.Fatal(err)
		}
		_ = i
	}
	endpointSites := []string{f.sites[0], f.sites[1], f.sites[1], f.sites[2]}
	feederLengths := []float64{30, 30, 25, 25}
	for i, id := range f.ends {
		ep := model.Endpoint{
			ID: id, SiteID: endpointSites[i], Name: id,
			RadioModelID: f.radio, AntennaModelID: f.antenna,
		}
		if err := st.CreateEndpoint(ctx, ep); err != nil {
			t.Fatal(err)
		}
		f.mustPublish(params.LayerSite, "endpoint_radio", id, map[string]float64{
			params.FieldInstallationFeederM: feederLengths[i],
		})
	}
	f.createLink(f.links[0], f.ends[0], f.ends[1], f.sites[0], f.sites[1], 10)
	f.createLink(f.links[1], f.ends[2], f.ends[3], f.sites[1], f.sites[2], 12)
	if err := st.CreateChannel(ctx, model.Channel{
		ID: f.channel, Name: f.channel, LinkIDs: []string{f.links[0], f.links[1]},
		TargetAvailability: 0.9999, CorrelationRho: 0.6,
	}); err != nil {
		t.Fatal(err)
	}

	// Drain the tasks produced by initial publications before the management
	// assertions begin.
	f.drainQueuedTasks()
	return f
}

func (f *integrationFixture) createLink(id, endA, endB, siteA, siteB string, distance float64) {
	if err := f.st.CreateLink(f.ctx, model.Link{
		ID: id, Name: id, EndAID: endA, EndBID: endB, SiteAID: siteA, SiteBID: siteB,
		FrequencyGHz: 18.1, DistanceKM: distance, Polarization: linkbudget.PolarizationHorizontal,
		RainRegionID: f.region, RainBandID: f.coefficient, TargetAvailability: 0.9999,
	}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *integrationFixture) mustPublish(layer params.Layer, kind, id string, fields map[string]float64) *model.RecomputeTask {
	f.t.Helper()
	if _, err := f.st.PutDraft(f.ctx, store.PutDraftRequest{
		Layer: layer, ScopeKind: kind, ScopeID: id, Fields: fields, BaseRevision: 0,
	}); err != nil {
		f.t.Fatalf("put draft %s/%s/%s: %v", layer, kind, id, err)
	}
	_, task, err := f.mgr.PublishAndAffect(f.ctx, layer, kind, id, "tester", "fixture")
	if err != nil {
		f.t.Fatalf("publish %s/%s/%s: %v", layer, kind, id, err)
	}
	return task
}

func (f *integrationFixture) drainQueuedTasks() {
	f.t.Helper()
	for {
		id, err := f.st.OldestQueuedTask(f.ctx)
		if err != nil {
			f.t.Fatal(err)
		}
		if id == "" {
			return
		}
		if err := f.mgr.runOne(f.ctx, id); err != nil {
			f.t.Fatal(err)
		}
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// TestPublishImpactRecomputeFollowupAndReplay is the end-to-end property test:
// exact affected set, frozen task versions, one follow-up when a publication
// happens during execution, target crossing, and exact historical replay.
func TestPublishImpactRecomputeFollowupAndReplay(t *testing.T) {
	f := newIntegrationFixture(t)

	baseline1, err := f.svc.EvaluateLink(f.ctx, f.links[0], "instant", "", true)
	if err != nil {
		t.Fatal(err)
	}
	baselineChannel, _, err := f.svc.EvaluateChannel(f.ctx, f.channel, "instant", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !baselineChannel.MeetsTarget {
		t.Fatalf("baseline channel must meet target, availability %.8f", baselineChannel.Combined.SelectedAvailability)
	}

	// Publishing a different, unreferenced radio model must leave the link out
	// of the affected set and must not change its historical result.
	unrelated := f.prefix + "_unrelated_radio"
	unrelatedTask := f.mustPublish(params.LayerNominal, "radio_model", unrelated, map[string]float64{
		params.FieldRadioTxPowerDBm:      99,
		params.FieldRadioRxThresholdDBm:  -99,
		params.FieldRadioMinFrequencyGHz: 1,
		params.FieldRadioMaxFrequencyGHz: 100,
		params.FieldRadioFeederLossDBpm:  0,
	})
	if len(unrelatedTask.Affected.Endpoints)+len(unrelatedTask.Affected.Links)+len(unrelatedTask.Affected.Channels) != 0 {
		t.Fatalf("unrelated model produced impact: %+v", unrelatedTask.Affected)
	}
	if err := f.mgr.runOne(f.ctx, unrelatedTask.ID); err != nil {
		t.Fatal(err)
	}
	replayedBaseline, err := f.svc.ReplayLinkEvaluation(f.ctx, baseline1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !replayedBaseline.Reproducible {
		t.Fatalf("baseline did not reproduce after unrelated publish: %+v", replayedBaseline)
	}

	// A real nominal change affects exactly the four endpoints, two hops and
	// one channel that resolve this model.
	txTask := f.mustPublish(params.LayerNominal, "radio_model", f.radio, map[string]float64{
		params.FieldRadioTxPowerDBm:      5,
		params.FieldRadioRxThresholdDBm:  -85,
		params.FieldRadioMinFrequencyGHz: 17.5,
		params.FieldRadioMaxFrequencyGHz: 18.7,
		params.FieldRadioFeederLossDBpm:  0.12,
		params.FieldRadioSplitterLossDB:  0,
	})
	wantEndpoints := []string{f.ends[0], f.ends[1], f.ends[2], f.ends[3]}
	if len(txTask.Affected.Endpoints) != len(wantEndpoints) {
		t.Fatalf("affected endpoints = %v, want %v", txTask.Affected.Endpoints, wantEndpoints)
	}
	for _, want := range wantEndpoints {
		if !contains(txTask.Affected.Endpoints, want) {
			t.Fatalf("affected endpoints %v missing %s", txTask.Affected.Endpoints, want)
		}
	}
	if len(txTask.Affected.Links) != 2 || !contains(txTask.Affected.Links, f.links[0]) || !contains(txTask.Affected.Links, f.links[1]) {
		t.Fatalf("affected links wrong: %v", txTask.Affected.Links)
	}
	if len(txTask.Affected.Channels) != 1 || txTask.Affected.Channels[0] != f.channel {
		t.Fatalf("affected channels wrong: %v", txTask.Affected.Channels)
	}

	// Claim the task, then publish a new site version while it is running.
	claimed, err := f.st.MarkTaskRunning(f.ctx, txTask.ID)
	if err != nil || !claimed {
		t.Fatalf("claim task: claimed=%v err=%v", claimed, err)
	}
	siteTask := f.mustPublish(params.LayerSite, "endpoint_radio", f.ends[0], map[string]float64{
		params.FieldInstallationFeederM: 30,
		params.FieldInstallationExtraDB: 3,
	})
	if len(siteTask.Affected.Endpoints) != 1 || siteTask.Affected.Endpoints[0] != f.ends[0] {
		t.Fatalf("site publication impact wrong: %+v", siteTask.Affected)
	}

	if err := f.mgr.execute(f.ctx, txTask.ID); err != nil {
		t.Fatal(err)
	}
	done, err := f.st.GetTask(f.ctx, txTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusDone {
		t.Fatalf("task status = %s error=%s", done.Status, done.Error)
	}
	if len(done.Diff.MarginDecreased) != 2 {
		t.Fatalf("expected both margins to decrease, got %+v", done.Diff.MarginDecreased)
	}
	if len(done.Diff.ChannelsCrossedBelow) != 1 || done.Diff.ChannelsCrossedBelow[0].ChannelID != f.channel {
		t.Fatalf("expected channel target crossing, got %+v", done.Diff.ChannelsCrossedBelow)
	}
	if done.Diff.ChannelsCrossedBelow[0].BeforeAvailability < baselineChannel.Combined.SelectedAvailability-1e-12 {
		t.Fatal("diff before availability should equal the stored baseline availability")
	}

	// Exactly one newer task exists: the automatic site-publication task. No
	// redundant manual follow-up is stacked.
	queued, err := f.st.ListTasks(f.ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	var activeNewer int
	for _, task := range queued {
		if task.Status == StatusQueued && task.ID == siteTask.ID {
			activeNewer++
		}
		if task.Status == StatusQueued && task.Trigger == "manual" &&
			task.FollowupOf == txTask.ID {
			t.Fatal("runner should not stack a follow-up when a newer publish task is already queued")
		}
	}
	if activeNewer != 1 {
		t.Fatalf("expected exactly one queued newer publication task, got %d", activeNewer)
	}

	// The completed task used its frozen pre-site versions: its L1 result must
	// not include the 3 dB site extra loss. Its task-specific history can be
	// replayed exactly.
	taskLinkHistory, err := f.st.ListLinkEvaluations(f.ctx, f.links[0], 20)
	if err != nil {
		t.Fatal(err)
	}
	var taskEval *model.LinkEvaluation
	for i := range taskLinkHistory {
		if taskLinkHistory[i].TaskID == txTask.ID {
			taskEval = &taskLinkHistory[i]
			break
		}
	}
	if taskEval == nil {
		t.Fatal("missing persisted task hop evaluation")
	}
	expectedTaskMargin := baseline1.Budget.FadeMarginDB - 10
	if diff := taskEval.Budget.FadeMarginDB - expectedTaskMargin; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("frozen task margin %.6f, want %.6f (baseline %.6f)", taskEval.Budget.FadeMarginDB, expectedTaskMargin, baseline1.Budget.FadeMarginDB)
	}
	replayedTask, err := f.svc.ReplayLinkEvaluation(f.ctx, taskEval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !replayedTask.Reproducible {
		t.Fatalf("task evaluation did not reproduce: %+v", replayedTask)
	}

	// Now process the queued publication-during-execution task. Its frozen
	// snapshot includes the new site field and loses the additional 3 dB.
	if err := f.mgr.runOne(f.ctx, siteTask.ID); err != nil {
		t.Fatal(err)
	}
	siteDone, err := f.st.GetTask(f.ctx, siteTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if siteDone.Status != StatusDone {
		t.Fatalf("site task status=%s error=%s", siteDone.Status, siteDone.Error)
	}
	if len(siteDone.Diff.MarginDecreased) != 1 || siteDone.Diff.MarginDecreased[0].LinkID != f.links[0] {
		t.Fatalf("site task diff should contain only L1: %+v", siteDone.Diff)
	}
	if mathAbs(siteDone.Diff.MarginDecreased[0].DeltaDB+3) > 1e-9 {
		t.Fatalf("extra site loss delta = %.6f, want -3", siteDone.Diff.MarginDecreased[0].DeltaDB)
	}
}

func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// TestDraftConflict verifies optimistic concurrency on the same layer/scope.
func TestDraftConflict(t *testing.T) {
	f := newIntegrationFixture(t)
	layer := params.LayerNominal
	kind := "radio_model"
	id := f.prefix + "_conflict_radio"
	fields := map[string]float64{
		params.FieldRadioTxPowerDBm:      10,
		params.FieldRadioRxThresholdDBm:  -80,
		params.FieldRadioMinFrequencyGHz: 17,
		params.FieldRadioMaxFrequencyGHz: 19,
		params.FieldRadioFeederLossDBpm:  0.1,
	}
	first, err := f.st.PutDraft(f.ctx, store.PutDraftRequest{Layer: layer, ScopeKind: kind, ScopeID: id, Fields: fields, BaseRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.PutDraft(f.ctx, store.PutDraftRequest{Layer: layer, ScopeKind: kind, ScopeID: id, Fields: fields, BaseRevision: 0}); err == nil {
		t.Fatal("second writer using stale revision zero must receive a conflict")
	}
	second, err := f.st.PutDraft(f.ctx, store.PutDraftRequest{
		Layer: layer, ScopeKind: kind, ScopeID: id,
		Fields: map[string]float64{params.FieldRadioTxPowerDBm: 11}, BaseRevision: first.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != first.Revision+1 {
		t.Fatalf("revision = %d, want %d", second.Revision, first.Revision+1)
	}
}

// TestReferenceValidation verifies missing model/version and non-contiguous
// channel topology errors point at the relevant concept rather than panicking.
func TestReferenceValidation(t *testing.T) {
	f := newIntegrationFixture(t)
	if err := f.st.CreateEndpoint(f.ctx, model.Endpoint{
		ID: f.prefix + "_bad_ep", SiteID: f.sites[0], Name: "bad",
		RadioModelID: f.prefix + "_missing", AntennaModelID: f.antenna,
	}); err == nil {
		t.Fatal("creating an endpoint with a missing radio model must fail")
	}
	if _, err := f.st.SetAtVersion(f.ctx, params.LayerNominal, "radio_model", f.radio, 999_999); err == nil {
		t.Fatal("referencing a nonexistent version must fail")
	}
	badChannel := model.Channel{
		ID: f.prefix + "_bad_channel", Name: "bad",
		LinkIDs: []string{f.links[0], f.prefix + "_missing_link"},
	}
	if err := f.svc.ValidateTopology(f.ctx, &badChannel); err == nil {
		t.Fatal("channel referencing a missing link must fail")
	}
	nonContiguous := model.Channel{
		ID: f.prefix + "_non_contiguous", Name: "noncontig",
		// L1 is S1-S2; farLink is S3-S4, so the two adjacent links share no
		// site and the channel must be rejected with a field-level error.
		LinkIDs: []string{f.links[0], f.prefix + "_far_link"},
	}
	site4 := f.prefix + "_s4"
	if err := f.st.CreateSite(f.ctx, model.Site{ID: site4, Name: site4, Region: f.region}); err != nil {
		t.Fatal(err)
	}
	ep5 := f.prefix + "_e5"
	ep6 := f.prefix + "_e6"
	for _, id := range []string{ep5, ep6} {
		site := f.sites[2]
		if id == ep6 {
			site = site4
		}
		if err := f.st.CreateEndpoint(f.ctx, model.Endpoint{
			ID: id, SiteID: site, Name: id,
			RadioModelID: f.radio, AntennaModelID: f.antenna,
		}); err != nil {
			t.Fatal(err)
		}
	}
	farLink := f.prefix + "_far_link"
	if err := f.st.CreateLink(f.ctx, model.Link{
		ID: farLink, Name: farLink, EndAID: ep5, EndBID: ep6,
		SiteAID: f.sites[2], SiteBID: site4, FrequencyGHz: 18.1, DistanceKM: 8,
		Polarization: linkbudget.PolarizationHorizontal, RainRegionID: f.region,
		RainBandID: f.coefficient,
	}); err != nil {
		t.Fatal(err)
	}
	err := f.svc.ValidateTopology(f.ctx, &nonContiguous)
	if err == nil {
		t.Fatal("non-contiguous adjacent links must be rejected")
	}
	var validation *linkbudget.ValidationErrors
	if !asValidationError(err, &validation) {
		t.Fatalf("expected validation errors, got %T %v", err, err)
	}
	if len(validation.Errors) == 0 || validation.Errors[0].Field != "linkIds[1]" {
		t.Fatalf("expected field linkIds[1], got %+v", validation.Errors)
	}
}

func asValidationError(err error, target **linkbudget.ValidationErrors) bool {
	v, ok := err.(*linkbudget.ValidationErrors)
	if !ok {
		return false
	}
	*target = v
	return true
}
