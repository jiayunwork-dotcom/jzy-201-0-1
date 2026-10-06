package service

import (
	"context"
	"time"

	"microwave-network/internal/budget"
	"microwave-network/internal/channel"
	"microwave-network/internal/domain"
	"microwave-network/internal/params"
	"microwave-network/internal/rain"
	"microwave-network/internal/store"
)

const (
	colAssessments = "assessments"
	adjacentRho    = 0.5
)

type LinkAssessment struct {
	LinkID            string              `json:"link_id"`
	Link              domain.Link         `json:"link"`
	Budget            budget.Result       `json:"budget"`
	RainExceedancePct float64             `json:"rain_exceedance_pct"`
	Availability      float64             `json:"availability"`
	ClimateZone       domain.ClimateZone  `json:"climate_zone"`
	RainBand          domain.RainBand     `json:"rain_band"`
	VersionRefs       []domain.VersionRef `json:"version_refs"`
}

type ChannelAssessment struct {
	ChannelID   string              `json:"channel_id"`
	Channel     domain.Channel      `json:"channel"`
	Links       []LinkAssessment    `json:"links"`
	Independent channel.Comparison  `json:"comparison"`
	MeetsTarget bool                `json:"meets_target"`
	VersionRefs []domain.VersionRef `json:"version_refs"`
}

type LinkComparison struct {
	OldMarginDB     float64 `json:"old_margin_db"`
	NewMarginDB     float64 `json:"new_margin_db"`
	DeltaDB         float64 `json:"delta_db"`
	OldAvailability float64 `json:"old_availability"`
	NewAvailability float64 `json:"new_availability"`
}

type ChannelComparisonResult struct {
	OldAvailable   bool    `json:"old_available"`
	NewAvailable   bool    `json:"new_available"`
	OldIndependent float64 `json:"old_independent_availability"`
	NewIndependent float64 `json:"new_independent_availability"`
	Target         float64 `json:"target"`
}

type AssessmentComparison struct {
	Kind    string                   `json:"kind"`
	Link    *LinkComparison          `json:"link,omitempty"`
	Channel *ChannelComparisonResult `json:"channel,omitempty"`
}

type AssessmentRecord struct {
	ID          string              `json:"id"`
	Kind        string              `json:"kind"` // link or channel
	SubjectID   string              `json:"subject_id"`
	JobID       string              `json:"job_id,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	VersionRefs []domain.VersionRef `json:"version_refs"`
	Link        *LinkAssessment     `json:"link,omitempty"`
	Channel     *ChannelAssessment  `json:"channel,omitempty"`
}

type Evaluator struct {
	store store.Store
	ps    *params.Service
	top   *Topology
}

func NewEvaluator(s store.Store, ps *params.Service, top *Topology) *Evaluator {
	return &Evaluator{store: s, ps: ps, top: top}
}

// EvaluateLink evaluates one link against explicit refs. Passing latest refs is
// the common case; historical refs make any past assessment reproducible.
func (e *Evaluator) EvaluateLink(ctx context.Context, l domain.Link, refs []domain.VersionRef) (LinkAssessment, error) {
	var out LinkAssessment
	err := e.store.RunTx(ctx, func(tx store.Tx) error {
		var zone domain.ClimateZone
		if err := tx.GetDoc(ctx, "climate_zones", l.ClimateZoneID, &zone); err != nil {
			return err
		}
		var band domain.RainBand
		if err := tx.GetDoc(ctx, "rain_bands", l.RainBandID, &band); err != nil {
			return err
		}
		ea, err := params.EffectiveWithStore(ctx, tx, l.A, refs)
		if err != nil {
			return err
		}
		eb, err := params.EffectiveWithStore(ctx, tx, l.B, refs)
		if err != nil {
			return err
		}
		br, err := budget.Evaluate(l, ea, eb)
		if err != nil {
			return err
		}
		p := rain.ExceedancePercent(zone, band, l.A.FrequencyMHz, l.DistanceKm, br.WorstFadeMarginDB)
		out = LinkAssessment{LinkID: l.ID, Link: l, Budget: br, RainExceedancePct: p,
			Availability: 1 - p/100, ClimateZone: zone, RainBand: band, VersionRefs: store.VersionRefs(refs)}
		return nil
	})
	return out, err
}

func (e *Evaluator) EvaluateChannel(ctx context.Context, c domain.Channel, refs []domain.VersionRef) (ChannelAssessment, error) {
	out := ChannelAssessment{ChannelID: c.ID, Channel: c, VersionRefs: store.VersionRefs(refs)}
	ps := make([]float64, 0, len(c.LinkIDs))
	for _, id := range c.LinkIDs {
		l, err := e.top.GetLink(ctx, id)
		if err != nil {
			return out, err
		}
		a, err := e.EvaluateLink(ctx, l, refs)
		if err != nil {
			return out, err
		}
		out.Links = append(out.Links, a)
		ps = append(ps, a.RainExceedancePct)
	}
	out.Independent = channel.Compare(ps, adjacentRho)
	out.MeetsTarget = out.Independent.IndependentAvailability >= c.TargetAvailability
	return out, nil
}

// EvaluateChannelAssessments combines already computed hop assessments. This
// is used in batch jobs and historical replays without re-reading mutable links.
func ChannelAssessmentFromHops(c domain.Channel, hops []LinkAssessment) ChannelAssessment {
	ps := make([]float64, 0, len(hops))
	for _, h := range hops {
		ps = append(ps, h.RainExceedancePct)
	}
	out := ChannelAssessment{ChannelID: c.ID, Channel: c, Links: hops, Independent: channel.Compare(ps, adjacentRho)}
	out.MeetsTarget = out.Independent.IndependentAvailability >= c.TargetAvailability
	if len(hops) > 0 {
		out.VersionRefs = hops[0].VersionRefs
	}
	return out
}

// EvaluateAndSave performs an instant evaluation and stores it as a historical
// record. The complete record contains both input topology and version refs.
func (e *Evaluator) EvaluateAndSaveLink(ctx context.Context, linkID string, refs []domain.VersionRef) (AssessmentRecord, error) {
	l, err := e.top.GetLink(ctx, linkID)
	if err != nil {
		return AssessmentRecord{}, err
	}
	a, err := e.EvaluateLink(ctx, l, refs)
	if err != nil {
		return AssessmentRecord{}, err
	}
	id, err := e.store.NextID(ctx, colAssessments)
	if err != nil {
		return AssessmentRecord{}, err
	}
	rec := AssessmentRecord{ID: id, Kind: "link", SubjectID: linkID, CreatedAt: e.store.Now(), VersionRefs: a.VersionRefs, Link: &a}
	return rec, e.store.PutDoc(ctx, colAssessments, id, rec)
}

func (e *Evaluator) EvaluateAndSaveChannel(ctx context.Context, channelID string, refs []domain.VersionRef) (AssessmentRecord, error) {
	c, err := e.top.GetChannel(ctx, channelID)
	if err != nil {
		return AssessmentRecord{}, err
	}
	a, err := e.EvaluateChannel(ctx, c, refs)
	if err != nil {
		return AssessmentRecord{}, err
	}
	id, err := e.store.NextID(ctx, colAssessments)
	if err != nil {
		return AssessmentRecord{}, err
	}
	rec := AssessmentRecord{ID: id, Kind: "channel", SubjectID: channelID, CreatedAt: e.store.Now(), VersionRefs: a.VersionRefs, Channel: &a}
	return rec, e.store.PutDoc(ctx, colAssessments, id, rec)
}

func (e *Evaluator) GetAssessment(ctx context.Context, id string) (AssessmentRecord, error) {
	var r AssessmentRecord
	return r, e.store.GetDoc(ctx, colAssessments, id, &r)
}
func (e *Evaluator) ListAssessments(ctx context.Context, subject string) ([]AssessmentRecord, error) {
	var all []AssessmentRecord
	if err := e.store.ListDocs(ctx, colAssessments, &all); err != nil {
		return nil, err
	}
	if subject == "" {
		return all, nil
	}
	var out []AssessmentRecord
	for _, r := range all {
		if r.SubjectID == subject {
			out = append(out, r)
		}
	}
	return out, nil
}

func (e *Evaluator) CompareAssessments(ctx context.Context, leftID, rightID string) (AssessmentComparison, error) {
	left, err := e.GetAssessment(ctx, leftID)
	if err != nil {
		return AssessmentComparison{}, err
	}
	right, err := e.GetAssessment(ctx, rightID)
	if err != nil {
		return AssessmentComparison{}, err
	}
	if left.Kind != right.Kind {
		return AssessmentComparison{}, domain.Invalid(domain.FieldError{Field: "left/right", Message: "assessments must have the same kind"})
	}
	if left.Kind == "link" {
		return AssessmentComparison{Kind: "link", Link: &LinkComparison{
			OldMarginDB:     left.Link.Budget.WorstFadeMarginDB,
			NewMarginDB:     right.Link.Budget.WorstFadeMarginDB,
			DeltaDB:         right.Link.Budget.WorstFadeMarginDB - left.Link.Budget.WorstFadeMarginDB,
			OldAvailability: left.Link.Availability,
			NewAvailability: right.Link.Availability,
		}}, nil
	}
	return AssessmentComparison{Kind: "channel", Channel: &ChannelComparisonResult{
		OldAvailable:   left.Channel.MeetsTarget,
		NewAvailable:   right.Channel.MeetsTarget,
		OldIndependent: left.Channel.Independent.IndependentAvailability,
		NewIndependent: right.Channel.Independent.IndependentAvailability,
		Target:         right.Channel.Channel.TargetAvailability,
	}}, nil
}

// Replay re-evaluates using the exact topology stored in the historical record
// and version refs stored with it. It does not follow current mutable topology.
func (e *Evaluator) Replay(ctx context.Context, id string) (AssessmentRecord, error) {
	old, err := e.GetAssessment(ctx, id)
	if err != nil {
		return old, err
	}
	if old.Link != nil {
		br, err := budget.Evaluate(old.Link.Link, old.Link.Budget.EffectiveA, old.Link.Budget.EffectiveB)
		if err != nil {
			return old, err
		}
		p := rain.ExceedancePercent(old.Link.ClimateZone, old.Link.RainBand, old.Link.Link.A.FrequencyMHz, old.Link.Link.DistanceKm, br.WorstFadeMarginDB)
		a := *old.Link
		a.Budget = br
		a.RainExceedancePct = p
		a.Availability = 1 - p/100
		old.Link = &a
	}
	if old.Channel != nil {
		hops := make([]LinkAssessment, 0, len(old.Channel.Links))
		for _, stored := range old.Channel.Links {
			br, err := budget.Evaluate(stored.Link, stored.Budget.EffectiveA, stored.Budget.EffectiveB)
			if err != nil {
				return old, err
			}
			p := rain.ExceedancePercent(stored.ClimateZone, stored.RainBand, stored.Link.A.FrequencyMHz, stored.Link.DistanceKm, br.WorstFadeMarginDB)
			stored.Budget = br
			stored.RainExceedancePct = p
			stored.Availability = 1 - p/100
			hops = append(hops, stored)
		}
		a := ChannelAssessmentFromHops(old.Channel.Channel, hops)
		a.VersionRefs = old.VersionRefs
		old.Channel = &a
	}
	return old, nil
}
