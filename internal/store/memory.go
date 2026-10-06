package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"microwave-network/internal/domain"
)

type Memory struct {
	mu      sync.Mutex
	docs    map[string]map[string]json.RawMessage
	counter map[string]int64
	now     func() time.Time
}

func NewMemory() *Memory {
	return &Memory{docs: map[string]map[string]json.RawMessage{}, counter: map[string]int64{}, now: time.Now}
}

func (m *Memory) SetNow(f func() time.Time) { m.now = f }
func (m *Memory) Now() time.Time            { return m.now().UTC() }

func (m *Memory) RunTx(ctx context.Context, fn func(Tx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := map[string]map[string]json.RawMessage{}
	for c, docs := range m.docs {
		snapshot[c] = map[string]json.RawMessage{}
		for k, v := range docs {
			snapshot[c][k] = append(json.RawMessage(nil), v...)
		}
	}
	tx := &memTx{m: m, docs: snapshot, counter: cloneCounter(m.counter)}
	if err := fn(tx); err != nil {
		return err
	}
	m.docs = tx.docs
	m.counter = tx.counter
	return nil
}

type memTx struct {
	m       *Memory
	docs    map[string]map[string]json.RawMessage
	counter map[string]int64
}

func (m *Memory) PutDoc(ctx context.Context, collection, id string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return putDoc(m.docs, collection, id, value)
}
func (tx *memTx) PutDoc(ctx context.Context, collection, id string, value any) error {
	return putDoc(tx.docs, collection, id, value)
}
func putDoc(docs map[string]map[string]json.RawMessage, collection, id string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if docs[collection] == nil {
		docs[collection] = map[string]json.RawMessage{}
	}
	docs[collection][id] = raw
	return nil
}

func (m *Memory) GetDoc(ctx context.Context, collection, id string, dst any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return getDoc(m.docs, collection, id, dst)
}
func (tx *memTx) GetDoc(ctx context.Context, collection, id string, dst any) error {
	return getDoc(tx.docs, collection, id, dst)
}
func getDoc(docs map[string]map[string]json.RawMessage, collection, id string, dst any) error {
	raw, ok := docs[collection][id]
	if !ok {
		return domain.ErrNotFound
	}
	return json.Unmarshal(raw, dst)
}

func (m *Memory) DeleteDoc(ctx context.Context, collection, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return deleteDoc(m.docs, collection, id)
}
func (tx *memTx) DeleteDoc(ctx context.Context, collection, id string) error {
	return deleteDoc(tx.docs, collection, id)
}
func deleteDoc(docs map[string]map[string]json.RawMessage, collection, id string) error {
	c, ok := docs[collection]
	if !ok {
		return domain.ErrNotFound
	}
	if _, ok := c[id]; !ok {
		return domain.ErrNotFound
	}
	delete(c, id)
	return nil
}

func (m *Memory) ListDocs(ctx context.Context, collection string, dst any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return listDocs(m.docs, collection, dst)
}
func (tx *memTx) ListDocs(ctx context.Context, collection string, dst any) error {
	return listDocs(tx.docs, collection, dst)
}
func listDocs(docs map[string]map[string]json.RawMessage, collection string, dst any) error {
	ids := make([]string, 0, len(docs[collection]))
	for id := range docs[collection] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		items = append(items, docs[collection][id])
	}
	return json.Unmarshal(mustJSON(items), dst)
}

func (m *Memory) RevisionOf(ctx context.Context, collection, id string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return revisionOf(m.docs, collection, id)
}
func (tx *memTx) RevisionOf(ctx context.Context, collection, id string) (int64, error) {
	return revisionOf(tx.docs, collection, id)
}
func revisionOf(docs map[string]map[string]json.RawMessage, collection, id string) (int64, error) {
	var doc struct {
		Revision int64 `json:"revision"`
	}
	if err := getDoc(docs, collection, id, &doc); err != nil {
		return 0, err
	}
	return doc.Revision, nil
}

func (m *Memory) NextID(ctx context.Context, collection string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter[collection]++
	return fmt.Sprintf("%s-%d", collection, m.counter[collection]), nil
}
func (tx *memTx) NextID(ctx context.Context, collection string) (string, error) {
	tx.counter[collection]++
	return fmt.Sprintf("%s-%d", collection, tx.counter[collection]), nil
}

func cloneCounter(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

var _ Store = (*Memory)(nil)

func IsNotFound(err error) bool { return errors.Is(err, domain.ErrNotFound) }
