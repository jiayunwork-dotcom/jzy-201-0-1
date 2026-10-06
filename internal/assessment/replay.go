package assessment

import (
	"context"
	"fmt"
	"math"

	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/model"
	"microwaveplanner/internal/params"
	"microwaveplanner/internal/rain"
)

// ReplayComparison is the result of re-running a stored hop evaluation.
type ReplayComparison struct {
	Stored        *model.LinkEvaluation `json:"stored"`
	Recomputed    *model.LinkEvaluation `json:"recomputed"`
	BudgetMatches bool                  `json:"budgetMatches"`
	RainMatches   bool                  `json:"rainMatches"`
	Reproducible  bool                  `json:"reproducible"`
	MaxAbsDiff    float64               `json:"maxAbsDiff"`
	Note          string                `json:"note"`
}

// ReplayLinkEvaluation reconstructs the hop from the exact frozen layer
// versions recorded in the historical evaluation and recomputes it. Any drift
// in the immutable param sets would surface as a mismatch.
func (s *Service) ReplayLinkEvaluation(ctx context.Context, id string) (*ReplayComparison, error) {
	stored, err := s.Store.GetLinkEvaluation(ctx, id)
	if err != nil {
		return nil, err
	}
	link, err := s.Store.GetLink(ctx, stored.LinkID)
	if err != nil {
		return nil, err
	}
	endSnap, err := endpointSnapshotFromRecord(stored.LayerVersions, "endA")
	if err != nil {
		return nil, err
	}
	endSnap.EndpointID = link.EndAID
	aEP, err := s.Store.GetEndpoint(ctx, link.EndAID)
	if err != nil {
		return nil, err
	}
	a, err := s.Store.SnapshotEndpoint(ctx, *aEP, endSnap)
	if err != nil {
		return nil, err
	}
	endSnapB, err := endpointSnapshotFromRecord(stored.LayerVersions, "endB")
	if err != nil {
		return nil, err
	}
	endSnapB.EndpointID = link.EndBID
	bEP, err := s.Store.GetEndpoint(ctx, link.EndBID)
	if err != nil {
		return nil, err
	}
	b, err := s.Store.SnapshotEndpoint(ctx, *bEP, endSnapB)
	if err != nil {
		return nil, err
	}

	budget, err := linkbudget.Evaluate(linkbudget.HopInput{
		LinkID: link.ID, EndA: a.EffectiveEndpoint, EndB: b.EffectiveEndpoint,
		FrequencyGHz: link.FrequencyGHz, DistanceKM: link.DistanceKM,
		Polarization: link.Polarization,
	})
	if err != nil {
		return nil, err
	}
	rh := rain.Hop{
		DistanceKM: link.DistanceKM, FrequencyGHz: link.FrequencyGHz,
		R001: stored.RainR001, K: stored.RainK, Alpha: stored.RainAlpha,
	}
	p, _ := rain.UnavailabilityPercent(rh, budget.FadeMarginDB)

	const tol = 1e-9
	marginDiff := math.Abs(budget.FadeMarginDB - stored.Budget.FadeMarginDB)
	rainDiff := math.Abs(p - stored.RainP)
	maxDiff := math.Max(marginDiff, rainDiff)
	budgetMatches := marginDiff <= tol
	rainMatches := rainDiff <= tol
	repro := &model.LinkEvaluation{
		LinkID: stored.LinkID, Budget: *budget, RainP: p,
		Availability: 1 - p/100, LayerVersions: stored.LayerVersions,
	}
	return &ReplayComparison{
		Stored: stored, Recomputed: repro,
		BudgetMatches: budgetMatches, RainMatches: rainMatches,
		Reproducible: budgetMatches && rainMatches, MaxAbsDiff: maxDiff,
		Note: "recomputed from the frozen per-endpoint layer versions and stored climate/coefficient snapshot",
	}, nil
}

func endpointSnapshotFromRecord(versions map[string]any, endKey string) (model.EndpointSnapshot, error) {
	raw, ok := versions[endKey]
	if !ok {
		return model.EndpointSnapshot{}, fmt.Errorf("historical evaluation missing %s layer references", endKey)
	}
	refsAny, ok := raw.([]any)
	if !ok {
		return model.EndpointSnapshot{}, fmt.Errorf("historical evaluation %s references have unexpected shape", endKey)
	}
	snap := model.EndpointSnapshot{}
	for _, item := range refsAny {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		layer, _ := m["layer"].(string)
		verNum, _ := m["version"].(float64)
		v := int64(verNum)
		switch {
		case kind == "radio" && layer == string(params.LayerNominal):
			snap.RadioNominalVersion = v
		case kind == "radio" && layer == string(params.LayerBatch):
			snap.RadioBatchVersion = v
		case kind == "radio" && layer == string(params.LayerSite):
			snap.RadioSiteVersion = v
		case kind == "antenna" && layer == string(params.LayerNominal):
			snap.AntennaNominalVersion = v
		case kind == "antenna" && layer == string(params.LayerBatch):
			snap.AntennaBatchVersion = v
		case kind == "antenna" && layer == string(params.LayerSite):
			snap.AntennaSiteVersion = v
		}
	}
	return snap, nil
}
