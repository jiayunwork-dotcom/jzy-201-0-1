// Package task manages background recomputation and the publish/impact boundary.
package task

import (
	"context"
	"log"
	"sync"
	"time"

	"microwaveplanner/internal/assessment"
	"microwaveplanner/internal/model"
	"microwaveplanner/internal/params"
	"microwaveplanner/internal/store"
)

// Status values for recompute tasks.
const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// Manager owns asynchronous recompute work.
type Manager struct {
	Store      *store.Store
	Assessment *assessment.Service
	workerOnce sync.Once
}

// NewManager creates a manager.
func NewManager(st *store.Store, as *assessment.Service) *Manager {
	return &Manager{Store: st, Assessment: as}
}

// Start launches the single background worker.
func (m *Manager) Start(ctx context.Context) {
	m.workerOnce.Do(func() {
		go m.workerLoop(ctx)
	})
}

func (m *Manager) workerLoop(ctx context.Context) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			id, err := m.Store.OldestQueuedTask(ctx)
			if err != nil {
				log.Printf("task queue scan: %v", err)
				continue
			}
			if id != "" {
				if err := m.runOne(ctx, id); err != nil {
					log.Printf("task %s: %v", id, err)
				}
			}
		}
	}
}

// PublishAndAffect publishes a draft atomically with exact impact analysis,
// frozen endpoint snapshots and an automatic recompute task.
func (m *Manager) PublishAndAffect(ctx context.Context, layer params.Layer, scopeKind, scopeID, user, comment string) (*store.PublishResult, *model.RecomputeTask, error) {
	return m.Store.PublishWithTask(ctx, layer, scopeKind, scopeID, user, comment)
}

// SubmitManual enqueues a full recompute against current heads and freezes
// every endpoint's set versions atomically.
func (m *Manager) SubmitManual(ctx context.Context, reason string) (*model.RecomputeTask, error) {
	if reason == "" {
		reason = "manual recomputation"
	}
	return m.Store.CreateManualTask(ctx, reason, "")
}

// RunOneForTest claims and executes one queued task synchronously. It exists
// for integration/HTTP tests that must not race the background worker; it is
// safe in production too but is intentionally named for tests.
func (m *Manager) RunOneForTest(ctx context.Context, id string) error {
	return m.runOne(ctx, id)
}
