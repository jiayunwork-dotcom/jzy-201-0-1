// Package version defines draft revisions and optimistic concurrency rules.
// Persistence performs the authoritative conditional update; these helpers
// make the rule explicit and testable.
package version

import "fmt"

// Draft is mutable layer content for one scope.
type Draft struct {
	Layer        string             `json:"layer"`
	ScopeKind    string             `json:"scopeKind"` // radio_model, antenna_model, radio_batch, ...
	ScopeID      string             `json:"scopeId"`
	Fields       map[string]float64 `json:"fields"`
	BaseRevision int64              `json:"baseRevision"`
	Revision     int64              `json:"revision"`
}

// Conflict describes a lost-update conflict.
type Conflict struct {
	Layer        string `json:"layer"`
	ScopeID      string `json:"scopeId"`
	YourRevision int64  `json:"yourRevision"`
	Current      int64  `json:"current"`
}

func (e *Conflict) Error() string {
	return fmt.Sprintf("draft for %s/%s changed: base revision %d is stale, current revision is %d",
		e.Layer, e.ScopeID, e.YourRevision, e.Current)
}

// CheckRevision enforces optimistic concurrency. expectedBase is the revision
// the client read; current is the revision held by the store.
func CheckRevision(expectedBase, current int64) error {
	if expectedBase != current {
		return &Conflict{YourRevision: expectedBase, Current: current}
	}
	return nil
}

// NextPublishedVersion enforces the immutable, monotonically increasing
// version policy for one scope. A publish always creates a new version even if
// its numeric fields are unchanged.
func NextPublishedVersion(latest int64) int64 { return latest + 1 }
