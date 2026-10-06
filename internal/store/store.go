package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"microwave-network/internal/domain"
)

type Tx interface {
	PutDoc(ctx context.Context, collection, id string, value any) error
	GetDoc(ctx context.Context, collection, id string, dst any) error
	DeleteDoc(ctx context.Context, collection, id string) error
	ListDocs(ctx context.Context, collection string, dst any) error
	// RevisionOf reads revision from documents that expose GetRevision().
	RevisionOf(ctx context.Context, collection, id string) (int64, error)
	NextID(ctx context.Context, collection string) (string, error)
}

type Store interface {
	Tx
	RunTx(ctx context.Context, fn func(Tx) error) error
	Now() time.Time
}

func Encode(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func Decode(raw json.RawMessage, dst any) error { return json.Unmarshal(raw, dst) }

type Revisioner interface{ GetRevision() int64 }

// VersionRefs returns deterministic, sorted layer references. It is used both
// for effective parameter lookups and assessment provenance.
func VersionRefs(refs []domain.VersionRef) []domain.VersionRef {
	out := append([]domain.VersionRef(nil), refs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Layer != out[j].Layer {
			return out[i].Layer < out[j].Layer
		}
		if out[i].ScopeID != out[j].ScopeID {
			return out[i].ScopeID < out[j].ScopeID
		}
		return out[i].Version < out[j].Version
	})
	return out
}
