// Package assessment evaluates hops and tandem channels and records every
// result with the exact layer versions used.
package assessment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"microwaveplanner/internal/channel"
	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/model"
	"microwaveplanner/internal/rain"
	"microwaveplanner/internal/store"
)

// Service performs domain evaluations.
type Service struct {
	Store *store.Store
}

// New creates an assessment service.
func New(st *store.Store) *Service { return &Service{Store: st} }

// HopContext contains all resolved inputs for one hop evaluation.
type HopContext struct {
	Link *model.Link
	EndA *model.EffectiveEndpointView
	EndB *model.EffectiveEndpointView
}

// LoadHop resolves a link and both ends at current head versions.
func (s *Service) LoadHop(ctx context.Context, linkID string) (*HopContext, error) {
	link, err := s.Store.GetLink(ctx, linkID)
	if err != nil {
		return nil, err
	}
	a, _, err := s.Store.ResolveEndpoint(ctx, link.EndAID)
	if err != nil {
		return nil, err
	}
	b, _, err := s.Store.ResolveEndpoint(ctx, link.EndBID)
	if err != nil {
		return nil, err
	}
	return &HopContext{Link: link, EndA: a, EndB: b}, nil
}

// ValidateTopology performs cross-reference and continuity checks beyond per-
// field types.
func (s *Service) ValidateTopology(ctx context.Context, ch *model.Channel) error {
	var errs []linkbudget.FieldError
	if len(ch.LinkIDs) == 0 {
		return &linkbudget.ValidationErrors{Errors: []linkbudget.FieldError{{
			Field: "linkIds", Message: "channel must contain at least one link",
		}}}
	}
	var prev *model.Link
	for i, id := range ch.LinkIDs {
		l, err := s.Store.GetLink(ctx, id)
		if err != nil {
			errs = append(errs, linkbudget.FieldError{
				Field: fmt.Sprintf("linkIds[%d]", i), Message: err.Error(),
			})
			continue
		}
		if prev != nil && !adjacentSitesShare(prev, l) {
			errs = append(errs, linkbudget.FieldError{
				Field:   fmt.Sprintf("linkIds[%d]", i),
				Message: fmt.Sprintf("adjacent links %s and %s do not share a site", prev.ID, l.ID),
			})
		}
		prev = l
	}
	if len(errs) > 0 {
		return &linkbudget.ValidationErrors{Errors: errs}
	}
	return nil
}

func adjacentSitesShare(a, b *model.Link) bool {
	return a.SiteAID == b.SiteAID || a.SiteAID == b.SiteBID ||
		a.SiteBID == b.SiteAID || a.SiteBID == b.SiteBID
}

// EvaluateLink evaluates, optionally persists, and returns a hop result.
func (s *Service) EvaluateLink(ctx context.Context, linkID, kind, taskID string, persist bool) (*model.LinkEvaluation, error) {
	hc, err := s.LoadHop(ctx, linkID)
	if err != nil {
		return nil, err
	}
	return s.evaluateLoadedHop(ctx, hc, kind, taskID, persist, nil)
}

// EvaluateLoadedHop evaluates a pre-loaded context.
func (s *Service) EvaluateLoadedHop(ctx context.Context, hc *HopContext, kind, taskID string, persist bool) (*model.LinkEvaluation, error) {
	return s.evaluateLoadedHop(ctx, hc, kind, taskID, persist, nil)
}

// evaluateLoadedHop evaluates a hop. When frozenHeads is non-nil the recorded
// global layer versions come from the recompute task's frozen snapshot rather
// than the current heads, so historical results reproduce exactly.
func (s *Service) evaluateLoadedHop(ctx context.Context, hc *HopContext, kind, taskID string, persist bool, frozenHeads *model.LayerVersions) (*model.LinkEvaluation, error) {
	budget, err := linkbudget.Evaluate(linkbudget.HopInput{
		LinkID: hc.Link.ID, SiteAID: hc.Link.SiteAID, SiteBID: hc.Link.SiteBID,
		EndA: hc.EndA.EffectiveEndpoint, EndB: hc.EndB.EffectiveEndpoint,
		FrequencyGHz: hc.Link.FrequencyGHz, DistanceKM: hc.Link.DistanceKM,
		Polarization: hc.Link.Polarization,
	})
	if err != nil {
		return nil, err
	}
	regionID := hc.Link.RainRegionID
	if regionID == "" {
		site, err := s.Store.GetSite(ctx, hc.Link.SiteAID)
		if err != nil {
			return nil, err
		}
		regionID = site.Region
	}
	climate, err := s.Store.GetRainClimate(ctx, regionID)
	if err != nil {
		return nil, err
	}
	bandID := hc.Link.RainBandID
	if bandID == "" {
		bandID = fmt.Sprintf("%.3fghz", hc.Link.FrequencyGHz)
	}
	coeff, err := s.Store.RainBandCoefficient(ctx, bandID, hc.Link.Polarization)
	if err != nil {
		return nil, err
	}
	rh := rain.Hop{
		DistanceKM: hc.Link.DistanceKM, FrequencyGHz: hc.Link.FrequencyGHz,
		R001: climate.R001, K: coeff.K, Alpha: coeff.Alpha,
	}
	pPercent, inRange := rain.UnavailabilityPercent(rh, budget.FadeMarginDB)
	availability := 1 - pPercent/100

	heads, err := s.Store.LayerHeads(ctx)
	if err != nil {
		return nil, err
	}
	recordedHeads := heads
	if frozenHeads != nil {
		recordedHeads = *frozenHeads
	}
	versions := endpointLayerVersions(hc)
	versions["_heads"] = recordedHeads

	inputHash := linkInputHash(hc, climate, coeff)
	result := &model.LinkEvaluation{
		ID: store.NewID("le"), LinkID: hc.Link.ID, Kind: kind, TaskID: taskID,
		EvaluatedAt: time.Now().UTC(), LayerVersions: versions, InputHash: inputHash,
		Budget: *budget, RainRegionID: climate.ID, RainR001: climate.R001,
		RainK: coeff.K, RainAlpha: coeff.Alpha, RainP: pPercent,
		Availability: availability, ValidRange: inRange,
		TargetAvailability: hc.Link.TargetAvailability,
		MeetsTarget:        hc.Link.TargetAvailability == 0 || availability >= hc.Link.TargetAvailability,
		Snapshot: map[string]any{
			"link":        hc.Link,
			"endA":        hc.EndA,
			"endB":        hc.EndB,
			"climate":     climate,
			"coefficient": coeff,
			"methods": []string{
				rain.MethodSpecificAttenuation,
				rain.MethodScaling,
				rain.MethodPathReduction,
			},
		},
	}
	if persist {
		if err := s.Store.SaveLinkEvaluation(ctx, *result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func endpointLayerVersions(hc *HopContext) map[string]any {
	return map[string]any{
		"endA": hc.EndA.Layers,
		"endB": hc.EndB.Layers,
	}
}

func canonicalJSON(v any) []byte {
	// Round-trip through interface maps then encode with sorted map keys.
	raw, _ := json.Marshal(v)
	var x any
	_ = json.Unmarshal(raw, &x)
	b, _ := json.Marshal(canonicalize(x))
	return b
}

func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := map[string]any{}
		for _, k := range keys {
			out[k] = canonicalize(t[k])
		}
		return out
	case []any:
		for i := range t {
			t[i] = canonicalize(t[i])
		}
		return t
	default:
		return v
	}
}

func sha(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func linkInputHash(hc *HopContext, climate *model.RainClimate, coeff *model.RainBandCoefficient) string {
	// Numeric effective parameters are hashed in endpoint-independent order to
	// make the hash invariant under A/B swapping.
	hashes := []string{hc.EndA.NumericHash(), hc.EndB.NumericHash()}
	sort.Strings(hashes)
	payload := struct {
		EndpointHashes [2]string
		Link           model.Link
		R001           float64
		K              float64
		Alpha          float64
	}{EndpointHashes: [2]string{hashes[0], hashes[1]}, Link: *hc.Link,
		R001: climate.R001, K: coeff.K, Alpha: coeff.Alpha}
	return sha(canonicalJSON(payload))
}

// EvaluateChannel combines current link rain unavailabilities and persists it.
func (s *Service) EvaluateChannel(ctx context.Context, channelID, kind, taskID string, persist bool) (*model.ChannelEvaluation, []*model.LinkEvaluation, error) {
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
		le, err := s.EvaluateLink(ctx, linkID, kind, taskID, persist)
		if err != nil {
			return nil, nil, err
		}
		hopEvals = append(hopEvals, le)
		hopP = append(hopP, le.RainP/100)
		versionMap[le.LinkID] = le.LayerVersions["_heads"]
		evalIDs = append(evalIDs, le.ID)
	}
	combined := channel.Combine(hopP, ch.CorrelationRho)
	meets := ch.TargetAvailability == 0 || combined.SelectedAvailability >= ch.TargetAvailability
	inputHash := sha(canonicalJSON(map[string]any{
		"channel": ch,
		"hops":    hopP,
		"evalIds": evalIDs,
	}))
	ce := &model.ChannelEvaluation{
		ID: store.NewID("ce"), ChannelID: ch.ID, Kind: kind, TaskID: taskID,
		EvaluatedAt: time.Now().UTC(), LayerVersions: versionMap, InputHash: inputHash,
		HopP: hopP, Combined: combined, TargetAvailability: ch.TargetAvailability,
		MeetsTarget: meets, LinkEvaluationIDs: evalIDs,
		Snapshot: map[string]any{"channel": ch, "linkEvaluations": hopEvals},
	}
	if persist {
		if err := s.Store.SaveChannelEvaluation(ctx, *ce); err != nil {
			return nil, nil, err
		}
	}
	return ce, hopEvals, nil
}
