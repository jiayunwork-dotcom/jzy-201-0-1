// Package params implements the three-layer equipment-parameter model and its
// deterministic merge.
//
// Layers, from low to high precedence:
//
//	nominal : vendor value for a radio or antenna model
//	batch   : measured correction for a purchased batch
//	site    : installation-specific override for one end of a link
//
// A higher layer contains only changed fields. Missing fields are inherited
// from the next lower layer. The merge is a fixed traversal of map keys, so
// the same three published versions produce the same value and provenance no
// matter in which order their drafts were entered.
package params

import (
	"fmt"
	"sort"
	"strings"

	"microwaveplanner/internal/num"
)

// Layer identifies one precedence layer.
type Layer string

const (
	LayerNominal Layer = "nominal"
	LayerBatch   Layer = "batch"
	LayerSite    Layer = "site"
)

// LayerOrder is the unique precedence order used by every merge.
var LayerOrder = []Layer{LayerNominal, LayerBatch, LayerSite}

// Ref identifies one immutable published set on one layer.
type Ref struct {
	Layer   Layer `json:"layer"`
	Version int64 `json:"version"`
	// ModelID identifies a radio/antenna model at the nominal layer.
	ModelID string `json:"modelId,omitempty"`
	// BatchID identifies a measured batch at the batch layer.
	BatchID string `json:"batchId,omitempty"`
	// EndpointID identifies one installed end at the site layer.
	EndpointID string `json:"endpointId,omitempty"`
}

func (r Ref) Key() string {
	return fmt.Sprintf("%s:%d:%s:%s:%s", r.Layer, r.Version, r.ModelID, r.BatchID, r.EndpointID)
}

// FieldSource states where an effective field value came from.
type FieldSource struct {
	Layer   Layer  `json:"layer"`
	Version int64  `json:"version"`
	SetID   string `json:"setId"`
}

// Set is one versioned set of parameters.
type Set struct {
	Ref    Ref                `json:"ref"`
	SetID  string             `json:"id"`
	Fields map[string]float64 `json:"fields"`
}

// Field is one effective parameter with provenance.
type Field struct {
	Name   string      `json:"name"`
	Value  float64     `json:"value"`
	Source FieldSource `json:"source"`
}

// EffectiveParams is the merged result for a logical scope.
type EffectiveParams struct {
	Fields map[string]Field `json:"fields"`
}

// VersionedSet groups layer sets supplied to a merge. Nil sets mean that the
// layer currently has no published set; their version is reported as zero.
type VersionedSet struct {
	Ref    Ref
	SetID  string
	Fields map[string]float64
}

// MergeFields merges fields in fixed low-to-high precedence.
//
// Only finite numbers participate in the merge. Site installation fields are
// not expected on nominal/batch radio sets; the same generic rule nevertheless
// handles any field because every field name has a global meaning.
func MergeFields(sets map[Layer]*VersionedSet) *EffectiveParams {
	out := &EffectiveParams{Fields: map[string]Field{}}
	for _, layer := range LayerOrder {
		vs := sets[layer]
		if vs == nil {
			continue
		}
		names := make([]string, 0, len(vs.Fields))
		for name, value := range vs.Fields {
			if !num.Finite(value) {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := vs.Fields[name]
			out.Fields[name] = Field{
				Name:  name,
				Value: value,
				Source: FieldSource{
					Layer:   layer,
					Version: vs.Ref.Version,
					SetID:   vs.SetID,
				},
			}
		}
	}
	return out
}

// Value returns a value and whether it exists.
func (e *EffectiveParams) Value(name string) (float64, bool) {
	f, ok := e.Fields[name]
	return f.Value, ok
}

// Hash returns a stable hash of effective field values. Source metadata is
// deliberately excluded: two evaluations compare equal when their numeric
// effective parameters are identical, even if provenance changed elsewhere.
func (e *EffectiveParams) Hash() string {
	parts := make([]string, 0, len(e.Fields))
	for name, f := range e.Fields {
		parts = append(parts, fmt.Sprintf("%s=%.12g", name, f.Value))
	}
	sort.Strings(parts)
	return stableSHA256(strings.Join(parts, "\n"))
}
