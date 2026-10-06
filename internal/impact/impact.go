// Package impact performs exact dependency-based impact analysis.
//
// An endpoint is affected only when a published set it actually resolves
// changes a field that contributes to its effective link-budget parameters.
// Therefore a nominal model changed on an unrelated model cannot place an
// endpoint or link in the affected set.
package impact

import (
	"microwaveplanner/internal/params"
)

// EndpointKey identifies one device end by all three lookup dimensions.
type EndpointKey struct {
	EndpointID   string
	RadioModelID string
	RadioBatchID string
	AntModelID   string
	AntBatchID   string
}

// AffectedLinks returns exactly the links containing at least one affected
// endpoint. The slice is sorted and de-duplicated.
func AffectedLinks(endpointToLinks map[string][]string, affectedEndpoints []string) []string {
	seen := map[string]struct{}{}
	var links []string
	for _, ep := range affectedEndpoints {
		for _, linkID := range endpointToLinks[ep] {
			if _, ok := seen[linkID]; ok {
				continue
			}
			seen[linkID] = struct{}{}
			links = append(links, linkID)
		}
	}
	return links
}

// AffectedChannels returns channels containing at least one affected link.
func AffectedChannels(linkToChannels map[string][]string, affectedLinks []string) []string {
	seen := map[string]struct{}{}
	var channels []string
	for _, linkID := range affectedLinks {
		for _, chID := range linkToChannels[linkID] {
			if _, ok := seen[chID]; ok {
				continue
			}
			seen[chID] = struct{}{}
			channels = append(channels, chID)
		}
	}
	return channels
}

// BudgetFieldNames identifies fields that can affect numeric link budget or
// rain unavailability. A revision touching only unrelated fields produces no
// impact even if an endpoint references that version.
var BudgetFieldNames = map[string]struct{}{
	params.FieldRadioTxPowerDBm:      {},
	params.FieldRadioRxThresholdDBm:  {},
	params.FieldRadioMinFrequencyGHz: {},
	params.FieldRadioMaxFrequencyGHz: {},
	params.FieldRadioFeederLossDBpm:  {},
	params.FieldRadioSplitterLossDB:  {},
	params.FieldAntennaGainDBD:       {},
	params.FieldInstallationFeederM:  {},
	params.FieldInstallationExtraDB:  {},
}

// RelevantFieldDelta compares two revisions and returns true if a budget field
// was added, deleted, or changed.
func RelevantFieldDelta(before, after map[string]float64) bool {
	names := map[string]struct{}{}
	for k := range before {
		names[k] = struct{}{}
	}
	for k := range after {
		names[k] = struct{}{}
	}
	for name := range names {
		if _, ok := BudgetFieldNames[name]; !ok {
			continue
		}
		vb, ob := before[name]
		va, oa := after[name]
		if ob != oa || vb != va {
			return true
		}
	}
	return false
}
