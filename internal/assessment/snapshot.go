package assessment

import (
	"context"
	"time"

	"microwaveplanner/internal/channel"
	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/model"
	"microwaveplanner/internal/store"
)

// SnapshotEvaluateLink evaluates a hop using exact frozen endpoint versions.
func (s *Service) SnapshotEvaluateLink(ctx context.Context, linkID, kind, taskID string,
	snapshots map[string]model.EndpointSnapshot, frozenHeads model.LayerVersions, persist bool) (*model.LinkEvaluation, error) {
	link, err := s.Store.GetLink(ctx, linkID)
	if err != nil {
		return nil, err
	}
	a, err := s.snapshotEndpoint(ctx, link.EndAID, snapshots)
	if err != nil {
		return nil, err
	}
	b, err := s.snapshotEndpoint(ctx, link.EndBID, snapshots)
	if err != nil {
		return nil, err
	}
	return s.evaluateLoadedHop(ctx, &HopContext{Link: link, EndA: a, EndB: b}, kind, taskID, persist, &frozenHeads)
}

func (s *Service) snapshotEndpoint(ctx context.Context, id string, snapshots map[string]model.EndpointSnapshot) (*model.EffectiveEndpointView, error) {
	snap, ok := snapshots[id]
	if !ok {
		return nil, &linkbudget.ValidationErrors{Errors: []linkbudget.FieldError{{
			Field: "endpointSnapshot", Message: "missing frozen snapshot for endpoint " + id,
		}}}
	}
	ep, err := s.Store.GetEndpoint(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.Store.SnapshotEndpoint(ctx, *ep, snap)
}

// SnapshotEvaluateChannelUsingLinks evaluates a tandem channel and reuses hop
// evaluations already performed by the task.
func (s *Service) SnapshotEvaluateChannelUsingLinks(ctx context.Context, channelID, kind, taskID string,
	snapshots map[string]model.EndpointSnapshot, frozenHeads model.LayerVersions,
	precomputed map[string]*model.LinkEvaluation, persist bool) (*model.ChannelEvaluation, []*model.LinkEvaluation, error) {
	ch, err := s.Store.GetChannel(ctx, channelID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.ValidateTopology(ctx, ch); err != nil {
		return nil, nil, err
	}
	hopEvals := make([]*model.LinkEvaluation, 0, len(ch.LinkIDs))
	hopP := make([]float64, 0, len(ch.LinkIDs))
	versionMap := map[string]any{}
	var evalIDs []string
	for _, linkID := range ch.LinkIDs {
		le := precomputed[linkID]
		if le == nil {
			le, err = s.SnapshotEvaluateLink(ctx, linkID, kind, taskID, snapshots, frozenHeads, persist)
			if err != nil {
				return nil, nil, err
			}
		}
		hopEvals = append(hopEvals, le)
		hopP = append(hopP, le.RainP/100)
		versionMap[le.LinkID] = le.LayerVersions
		evalIDs = append(evalIDs, le.ID)
	}
	combined := channel.Combine(hopP, ch.CorrelationRho)
	ce := &model.ChannelEvaluation{
		ID: store.NewID("ce"), ChannelID: ch.ID, Kind: kind, TaskID: taskID,
		EvaluatedAt: time.Now().UTC(), LayerVersions: versionMap,
		InputHash: sha(canonicalJSON(map[string]any{"channel": ch, "hops": hopP, "evalIds": evalIDs})),
		HopP:      hopP, Combined: combined, TargetAvailability: ch.TargetAvailability,
		MeetsTarget:       ch.TargetAvailability == 0 || combined.SelectedAvailability >= ch.TargetAvailability,
		LinkEvaluationIDs: evalIDs, Snapshot: map[string]any{"channel": ch, "linkEvaluations": hopEvals},
	}
	if persist {
		if err := s.Store.SaveChannelEvaluation(ctx, *ce); err != nil {
			return nil, nil, err
		}
	}
	return ce, hopEvals, nil
}

// SnapshotEvaluateChannel evaluates a tandem channel with frozen end versions.
func (s *Service) SnapshotEvaluateChannel(ctx context.Context, channelID, kind, taskID string,
	snapshots map[string]model.EndpointSnapshot, frozenHeads model.LayerVersions, persist bool) (*model.ChannelEvaluation, []*model.LinkEvaluation, error) {
	return s.SnapshotEvaluateChannelUsingLinks(ctx, channelID, kind, taskID, snapshots, frozenHeads, nil, persist)
}
