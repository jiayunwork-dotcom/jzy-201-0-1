package task

import (
	"context"

	"microwaveplanner/internal/model"
)

func (m *Manager) runOne(ctx context.Context, id string) error {
	claimed, err := m.Store.MarkTaskRunning(ctx, id)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	return m.execute(ctx, id)
}

// execute assumes the task has already been claimed. It is split out so the
// claim/run boundary (and therefore publication-during-execution) is testable
// without racing the worker.
func (m *Manager) execute(ctx context.Context, id string) error {
	t, err := m.Store.GetTask(ctx, id)
	if err != nil {
		return err
	}
	// Target holds the global layer heads captured atomically when the task was
	// submitted. The task always evaluates against these frozen versions.
	frozenHeads := t.Target

	beforeLinks := map[string]*model.LinkEvaluation{}
	beforeChannels := map[string]*model.ChannelEvaluation{}
	for _, linkID := range t.Affected.Links {
		le, err := m.Store.LatestLinkEvaluation(ctx, linkID)
		if err != nil {
			return m.fail(ctx, t, err)
		}
		if le != nil {
			beforeLinks[linkID] = le
		}
	}
	for _, chID := range t.Affected.Channels {
		ce, err := m.Store.LatestChannelEvaluation(ctx, chID)
		if err != nil {
			return m.fail(ctx, t, err)
		}
		if ce != nil {
			beforeChannels[chID] = ce
		}
	}

	snapshotRows, err := m.Store.TaskSnapshots(ctx, id)
	if err != nil {
		return m.fail(ctx, t, err)
	}
	snapshots := map[string]model.EndpointSnapshot{}
	for _, row := range snapshotRows {
		snapshots[row.EndpointID] = row
	}

	t.Progress = model.TaskProgress{
		TotalEndpoints: len(t.Affected.Endpoints),
		DoneEndpoints:  len(t.Affected.Endpoints),
		TotalLinks:     len(t.Affected.Links),
		TotalChannels:  len(t.Affected.Channels),
	}
	if err := m.Store.UpdateTaskProgress(ctx, id, t.Progress); err != nil {
		return m.fail(ctx, t, err)
	}

	afterLinks := map[string]*model.LinkEvaluation{}
	for _, linkID := range t.Affected.Links {
		le, err := m.Assessment.SnapshotEvaluateLink(ctx, linkID, "recompute", id, snapshots, frozenHeads, true)
		if err != nil {
			return m.fail(ctx, t, err)
		}
		afterLinks[linkID] = le
		t.Progress.DoneLinks++
		if err := m.Store.UpdateTaskProgress(ctx, id, t.Progress); err != nil {
			return m.fail(ctx, t, err)
		}
	}

	afterChannels := map[string]*model.ChannelEvaluation{}
	for _, chID := range t.Affected.Channels {
		ce, _, err := m.Assessment.SnapshotEvaluateChannelUsingLinks(ctx, chID, "recompute", id, snapshots, frozenHeads, afterLinks, true)
		if err != nil {
			return m.fail(ctx, t, err)
		}
		afterChannels[chID] = ce
		t.Progress.DoneChannels++
		if err := m.Store.UpdateTaskProgress(ctx, id, t.Progress); err != nil {
			return m.fail(ctx, t, err)
		}
	}

	t.Diff = buildDiff(beforeLinks, afterLinks, beforeChannels, afterChannels)
	t.Status = StatusDone
	// FinishTask preserves the task's frozen Baseline/Target: those describe
	// what the task actually computed. We only need current heads to decide
	// whether a publication happened during execution.
	heads, err := m.Store.LayerHeads(ctx)
	if err != nil {
		return m.fail(ctx, t, err)
	}
	if err := m.Store.FinishTask(ctx, t); err != nil {
		return err
	}

	// A current head different from the frozen target means at least one layer
	// was published while this task was queued/running. The running task kept
	// its frozen versions; now enqueue exactly one follow-up against the newest
	// state. Multiple intermediate publications collapse into this single
	// follow-up, and follow-ups form a chain via followupOf.
	if heads != frozenHeads {
		// Do not pile on a second follow-up if an automatic publish task (or a
		// previously scheduled follow-up) already covers the newer heads.
		hasNewer, err := m.Store.HasActiveTaskAfter(ctx, frozenHeads, id)
		if err != nil {
			return err
		}
		if !hasNewer {
			if _, err := m.Store.CreateManualTask(ctx, "follow-up after publication during task "+t.ID, t.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) fail(ctx context.Context, t *model.RecomputeTask, cause error) error {
	t.Status = StatusFailed
	t.Error = cause.Error()
	_ = m.Store.FinishTask(ctx, t)
	return cause
}

func buildDiff(beforeLinks map[string]*model.LinkEvaluation, afterLinks map[string]*model.LinkEvaluation,
	beforeChannels map[string]*model.ChannelEvaluation, afterChannels map[string]*model.ChannelEvaluation) model.TaskDiff {
	var d model.TaskDiff
	for id, after := range afterLinks {
		before, ok := beforeLinks[id]
		if !ok {
			continue
		}
		change := model.MarginChange{
			LinkID: id, BeforeMargin: before.Budget.FadeMarginDB,
			AfterMargin: after.Budget.FadeMarginDB,
			DeltaDB:     after.Budget.FadeMarginDB - before.Budget.FadeMarginDB,
		}
		if change.DeltaDB < 0 {
			d.MarginDecreased = append(d.MarginDecreased, change)
		} else if change.DeltaDB > 0 {
			d.MarginIncreased = append(d.MarginIncreased, change)
		}
	}
	for id, after := range afterChannels {
		before, ok := beforeChannels[id]
		if !ok {
			continue
		}
		change := model.ChannelAvailabilityChange{
			ChannelID:          id,
			BeforeAvailability: before.Combined.SelectedAvailability,
			AfterAvailability:  after.Combined.SelectedAvailability,
			Target:             after.TargetAvailability,
			BeforeMeetsTarget:  before.MeetsTarget,
			AfterMeetsTarget:   after.MeetsTarget,
		}
		if change.BeforeMeetsTarget && !change.AfterMeetsTarget {
			d.ChannelsCrossedBelow = append(d.ChannelsCrossedBelow, change)
		}
		if !change.BeforeMeetsTarget && change.AfterMeetsTarget {
			d.ChannelsRecovered = append(d.ChannelsRecovered, change)
		}
	}
	sortChanges(&d)
	return d
}

func sortChanges(d *model.TaskDiff) {
	marginLess := func(x []model.MarginChange, i, j int) bool { return x[i].LinkID < x[j].LinkID }
	channelLess := func(x []model.ChannelAvailabilityChange, i, j int) bool {
		return x[i].ChannelID < x[j].ChannelID
	}
	for i := 1; i < len(d.MarginDecreased); i++ {
		for j := i; j > 0 && marginLess(d.MarginDecreased, j, j-1); j-- {
			d.MarginDecreased[j-1], d.MarginDecreased[j] = d.MarginDecreased[j], d.MarginDecreased[j-1]
		}
	}
	for i := 1; i < len(d.MarginIncreased); i++ {
		for j := i; j > 0 && marginLess(d.MarginIncreased, j, j-1); j-- {
			d.MarginIncreased[j-1], d.MarginIncreased[j] = d.MarginIncreased[j], d.MarginIncreased[j-1]
		}
	}
	for i := 1; i < len(d.ChannelsCrossedBelow); i++ {
		for j := i; j > 0 && channelLess(d.ChannelsCrossedBelow, j, j-1); j-- {
			d.ChannelsCrossedBelow[j-1], d.ChannelsCrossedBelow[j] = d.ChannelsCrossedBelow[j], d.ChannelsCrossedBelow[j-1]
		}
	}
	for i := 1; i < len(d.ChannelsRecovered); i++ {
		for j := i; j > 0 && channelLess(d.ChannelsRecovered, j, j-1); j-- {
			d.ChannelsRecovered[j-1], d.ChannelsRecovered[j] = d.ChannelsRecovered[j], d.ChannelsRecovered[j-1]
		}
	}
}
