package impact

import (
	"reflect"
	"testing"

	"microwaveplanner/internal/params"
)

func TestAffectedGraphIsExact(t *testing.T) {
	endpointLinks := map[string][]string{
		"ep1": {"L1"},
		"ep2": {"L1", "L2"},
		"ep3": {"L3"},
		"ep4": nil,
	}
	links := AffectedLinks(endpointLinks, []string{"ep2"})
	if !reflect.DeepEqual(links, []string{"L1", "L2"}) {
		t.Fatalf("exact links = %v", links)
	}
	linkChannels := map[string][]string{"L1": {"C1", "C2"}, "L2": {"C2"}, "L3": {"C3"}}
	channels := AffectedChannels(linkChannels, links)
	if !reflect.DeepEqual(channels, []string{"C1", "C2"}) {
		t.Fatalf("exact channels = %v", channels)
	}
}

func TestRelevantFieldDelta(t *testing.T) {
	if RelevantFieldDelta(map[string]float64{params.FieldRadioTxPowerDBm: 20},
		map[string]float64{params.FieldRadioTxPowerDBm: 21}) != true {
		t.Fatal("changed budget field must be relevant")
	}
	if RelevantFieldDelta(map[string]float64{"unrelated": 1},
		map[string]float64{"unrelated": 2}) != false {
		t.Fatal("unrelated field must not trigger budget impact")
	}
	if RelevantFieldDelta(nil, map[string]float64{params.FieldInstallationExtraDB: 1}) != true {
		t.Fatal("added budget field must trigger impact")
	}
}
