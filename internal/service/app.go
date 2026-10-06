package service

import (
	"context"
	"reflect"
	"sort"

	"microwave-network/internal/domain"
	"microwave-network/internal/params"
	"microwave-network/internal/store"
	"microwave-network/internal/task"
)

type App struct {
	Store     store.Store
	Params    *params.Service
	Topology  *Topology
	Evaluator *Evaluator
	Tasks     *task.Manager
}

func NewApp(s store.Store) *App {
	ps := params.New(s)
	top := NewTopology(s)
	ev := NewEvaluator(s, ps, top)
	app := &App{Store: s, Params: ps, Topology: top, Evaluator: ev}
	app.Tasks = task.NewManager(s, app.runRecompute)
	return app
}

// PublishAndEnqueue atomically publishes a layer and enqueues an impact job
// pinned to the post-publication snapshot.
func (a *App) PublishAndEnqueue(ctx context.Context, req params.PublishRequest) (params.PublishResult, task.Job, error) {
	var pr params.PublishResult
	var j task.Job
	err := a.Store.RunTx(ctx, func(tx store.Tx) error {
		var err error
		pr, err = a.Params.PublishTx(ctx, tx, req)
		if err != nil {
			return err
		}
		id, err := tx.NextID(ctx, "recompute_jobs")
		if err != nil {
			return err
		}
		j = task.Job{ID: id, Status: task.StatusQueued, Reason: "layer_published",
			Change:    task.Change{Layer: req.Layer, ScopeID: req.ScopeID, Version: pr.Version.Version, PreviousVersion: pr.Version.Previous},
			StartRefs: pr.After, BeforeRefs: pr.Before, CreatedAt: a.Store.Now()}
		return tx.PutDoc(ctx, "recompute_jobs", id, j)
	})
	if err == nil {
		a.Tasks.NotifyExisting(j.ID)
	}
	return pr, j, err
}

func (a *App) runRecompute(ctx context.Context, j task.Job) (task.Result, error) {
	links, err := a.Topology.ListLinks(ctx)
	if err != nil {
		return task.Result{}, err
	}
	channels, err := a.Topology.ListChannels(ctx)
	if err != nil {
		return task.Result{}, err
	}
	refs := j.StartRefs
	if refs == nil {
		refs, err = a.Params.LatestRefs(ctx)
		if err != nil {
			return task.Result{}, err
		}
	}
	res := task.Result{}
	affectedLinks := map[string]domain.Link{}
	seenEndpoints := map[string]bool{}
	for _, l := range links {
		if !EffectiveChanged(l, j.Change.Layer, j.Change.ScopeID) {
			continue
		}
		// The scope may be attached to an endpoint yet publish a field that does
		// not alter this endpoint's effective values, so compare both snapshots.
		newEffA, errA := a.Evaluator.EvaluateLink(ctx, l, refs)
		if errA != nil {
			return res, errA
		}
		changed := j.Change.PreviousVersion == ""
		if j.Change.PreviousVersion != "" {
			oldEffA, err := a.Evaluator.EvaluateLink(ctx, l, j.BeforeRefs)
			if err == nil && !sameAssessment(newEffA, oldEffA) {
				changed = true
				res.AffectedEndpoints = appendAffectedEndpoints(res.AffectedEndpoints, seenEndpoints, l.ID, "a", newEffA.Budget.EffectiveA, oldEffA.Budget.EffectiveA)
				res.AffectedEndpoints = appendAffectedEndpoints(res.AffectedEndpoints, seenEndpoints, l.ID, "b", newEffA.Budget.EffectiveB, oldEffA.Budget.EffectiveB)
			}
		} else {
			res.AffectedEndpoints = appendAffectedEndpoints(res.AffectedEndpoints, seenEndpoints, l.ID, "a", newEffA.Budget.EffectiveA, params.EffectiveEndpoint{Endpoint: newEffA.Budget.EffectiveA.Endpoint})
			res.AffectedEndpoints = appendAffectedEndpoints(res.AffectedEndpoints, seenEndpoints, l.ID, "b", newEffA.Budget.EffectiveB, params.EffectiveEndpoint{Endpoint: newEffA.Budget.EffectiveB.Endpoint})
		}
		if changed {
			affectedLinks[l.ID] = l
		}
	}
	channelByID := map[string]domain.Channel{}
	affectedChannelIDs := []string{}
	for _, c := range channels {
		channelByID[c.ID] = c
		if channelTouchesLinks(c, affectedLinks) {
			affectedChannelIDs = append(affectedChannelIDs, c.ID)
		}
	}
	sort.Strings(affectedChannelIDs)
	j.Total = len(affectedLinks) + len(affectedChannelIDs)
	_ = a.Tasks.Save(ctx, j)

	assessed := map[string]LinkAssessment{}
	affectedIDs := make([]string, 0, len(affectedLinks))
	for id := range affectedLinks {
		affectedIDs = append(affectedIDs, id)
	}
	sort.Strings(affectedIDs)
	for _, id := range affectedIDs {
		l := affectedLinks[id]
		newA, err := a.Evaluator.EvaluateLink(ctx, l, refs)
		if err != nil {
			return res, err
		}
		if j.Change.PreviousVersion != "" {
			oldA, err := a.Evaluator.EvaluateLink(ctx, l, j.BeforeRefs)
			if err == nil && newA.Budget.WorstFadeMarginDB < oldA.Budget.WorstFadeMarginDB {
				res.MarginsReduced = append(res.MarginsReduced, task.MarginChange{
					LinkID: l.ID, OldMargin: oldA.Budget.WorstFadeMarginDB,
					NewMargin: newA.Budget.WorstFadeMarginDB,
					DeltaDB:   newA.Budget.WorstFadeMarginDB - oldA.Budget.WorstFadeMarginDB,
				})
			}
		}
		rec, err := a.Evaluator.EvaluateAndSaveLink(ctx, l.ID, refs)
		if err != nil {
			return res, err
		}
		rec.JobID = j.ID
		assessed[l.ID] = *rec.Link
		_ = a.Store.PutDoc(ctx, colAssessments, rec.ID, rec)
		res.LinksAssessed = append(res.LinksAssessed, l.ID)
		j.Progress++
		_ = a.Tasks.Save(ctx, j)
	}
	for _, cid := range affectedChannelIDs {
		c := channelByID[cid]
		hops := make([]LinkAssessment, 0, len(c.LinkIDs))
		for _, lid := range c.LinkIDs {
			if h, ok := assessed[lid]; ok {
				hops = append(hops, h)
				continue
			}
			l, err := a.Topology.GetLink(ctx, lid)
			if err != nil {
				return res, err
			}
			h, err := a.Evaluator.EvaluateLink(ctx, l, refs)
			if err != nil {
				return res, err
			}
			hops = append(hops, h)
		}
		newC := ChannelAssessmentFromHops(c, hops)
		newC.VersionRefs = refs
		id, err := a.Store.NextID(ctx, colAssessments)
		if err != nil {
			return res, err
		}
		rec := AssessmentRecord{ID: id, Kind: "channel", SubjectID: c.ID, JobID: j.ID, CreatedAt: a.Store.Now(), VersionRefs: refs, Channel: &newC}
		if err := a.Store.PutDoc(ctx, colAssessments, id, rec); err != nil {
			return res, err
		}
		res.ChannelsAssessed = append(res.ChannelsAssessed, c.ID)
		if !newC.MeetsTarget {
			res.ChannelsBelowTarget = append(res.ChannelsBelowTarget, c.ID)
		}
		j.Progress++
		_ = a.Tasks.Save(ctx, j)
	}
	// One follow-up catches any version published while this pinned job ran.
	latest, err := a.Params.LatestRefs(ctx)
	if err == nil && !sameRefs(latest, refs) && !a.Tasks.NewerQueuedOrRunning(ctx, j.ID) {
		fid, err := a.Store.NextID(ctx, "recompute_jobs")
		if err == nil {
			fj := task.Job{ID: fid, Status: task.StatusQueued, Reason: "followup_after_publication",
				FollowupFor: j.ID, StartRefs: latest, CreatedAt: a.Store.Now()}
			if err = a.Store.PutDoc(ctx, "recompute_jobs", fid, fj); err == nil {
				a.Tasks.NotifyExisting(fid)
			}
		}
	}
	return res, nil
}

// EffectiveChanged is exact: it tests whether the published scope can supply a
// field used by one of the link's endpoints.
func EffectiveChanged(l domain.Link, layer, scopeID string) bool {
	return endpointUses(l.A, layer, scopeID) || endpointUses(l.B, layer, scopeID)
}

func endpointUses(e domain.Endpoint, layer, scopeID string) bool {
	switch layer {
	case domain.LayerModel:
		return e.RadioModelID == scopeID || e.AntennaModelID == scopeID
	case domain.LayerBatch:
		return e.BatchID == scopeID
	case domain.LayerSite:
		return e.SiteID == scopeID
	}
	return false
}
func channelTouchesLinks(c domain.Channel, links map[string]domain.Link) bool {
	for _, id := range c.LinkIDs {
		if _, ok := links[id]; ok {
			return true
		}
	}
	return false
}
func refsWithVersion(refs []domain.VersionRef, layer, scope, version string) []domain.VersionRef {
	if version == "" {
		return nil
	}
	out := store.VersionRefs(refs)
	for i := range out {
		if out[i].Layer == layer && out[i].ScopeID == scope {
			out[i].Version = version
		}
	}
	return out
}
func appendAffectedEndpoints(out []task.AffectedEndpoint, seen map[string]bool, linkID, endpoint string, newE, oldE params.EffectiveEndpoint) []task.AffectedEndpoint {
	fields := make([]string, 0)
	for field, src := range newE.Sources {
		oldSrc := oldE.Sources[field]
		if src.Version != oldSrc.Version || src.Value != oldSrc.Value {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return out
	}
	sort.Strings(fields)
	e := newE.Endpoint
	key := e.SiteID + "|" + e.RadioModelID + "|" + e.AntennaModelID + "|" + e.BatchID
	if seen[key] {
		return out
	}
	seen[key] = true
	return append(out, task.AffectedEndpoint{
		LinkID: linkID, Endpoint: endpoint, SiteID: e.SiteID,
		RadioModelID: e.RadioModelID, AntennaModelID: e.AntennaModelID, ChangedFields: fields,
	})
}

func sameRefs(a, b []domain.VersionRef) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := store.VersionRefs(a), store.VersionRefs(b)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}
func sameAssessment(a, b LinkAssessment) bool {
	return reflect.DeepEqual(a.Budget.EffectiveA.Values, b.Budget.EffectiveA.Values) &&
		reflect.DeepEqual(a.Budget.EffectiveA.Sources, b.Budget.EffectiveA.Sources) &&
		reflect.DeepEqual(a.Budget.EffectiveB.Values, b.Budget.EffectiveB.Values) &&
		reflect.DeepEqual(a.Budget.EffectiveB.Sources, b.Budget.EffectiveB.Sources)
}
