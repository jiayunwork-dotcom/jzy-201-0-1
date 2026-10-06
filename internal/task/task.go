package task

import (
	"context"
	"sync"
	"time"

	"microwave-network/internal/domain"
	"microwave-network/internal/store"
)

const colJobs = "recompute_jobs"

type Status string

const (
	StatusQueued   Status = "queued"
	StatusRunning  Status = "running"
	StatusComplete Status = "complete"
	StatusFailed   Status = "failed"
)

type Job struct {
	ID           string              `json:"id"`
	Status       Status              `json:"status"`
	Progress     int                 `json:"progress"`
	Total        int                 `json:"total"`
	Reason       string              `json:"reason"`
	Change       Change              `json:"change"`
	StartRefs    []domain.VersionRef `json:"start_refs"`
	BeforeRefs   []domain.VersionRef `json:"before_refs,omitempty"`
	FollowupFor  string              `json:"followup_for,omitempty"`
	Result       Result              `json:"result"`
	ErrorMessage string              `json:"error_message,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	StartedAt    *time.Time          `json:"started_at,omitempty"`
	FinishedAt   *time.Time          `json:"finished_at,omitempty"`
}

type Change struct {
	Layer           string `json:"layer"`
	ScopeID         string `json:"scope_id"`
	Version         string `json:"version"`
	PreviousVersion string `json:"previous_version,omitempty"`
}

type AffectedEndpoint struct {
	LinkID         string   `json:"link_id"`
	Endpoint       string   `json:"endpoint"`
	SiteID         string   `json:"site_id"`
	RadioModelID   string   `json:"radio_model_id"`
	AntennaModelID string   `json:"antenna_model_id"`
	ChangedFields  []string `json:"changed_fields"`
}

type Result struct {
	AffectedEndpoints   []AffectedEndpoint `json:"affected_endpoints"`
	LinksAssessed       []string           `json:"links_assessed"`
	ChannelsAssessed    []string           `json:"channels_assessed"`
	MarginsReduced      []MarginChange     `json:"margins_reduced"`
	ChannelsBelowTarget []string           `json:"channels_below_target"`
}

type MarginChange struct {
	LinkID    string  `json:"link_id"`
	OldMargin float64 `json:"old_margin"`
	NewMargin float64 `json:"new_margin"`
	DeltaDB   float64 `json:"delta_db"`
}

type Handler func(context.Context, Job) (Result, error)

type Manager struct {
	st      store.Store
	mu      sync.Mutex
	queued  []string
	running bool
	handler Handler
	wake    chan struct{}
}

func NewManager(s store.Store, h Handler) *Manager {
	m := &Manager{st: s, handler: h, wake: make(chan struct{}, 1)}
	return m
}

// NotifyExisting registers a job that was created directly in a transaction.
func (m *Manager) NotifyExisting(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.queued {
		if x == id {
			return
		}
	}
	m.queued = append(m.queued, id)
}

// NewerQueuedOrRunning avoids stacking follow-up jobs during a burst.
func (m *Manager) NewerQueuedOrRunning(ctx context.Context, currentID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A follow-up is checked from inside the currently running job; therefore
	// only already-queued later jobs should suppress adding another one.
	return len(m.queued) > 0
}

func (m *Manager) Enqueue(ctx context.Context, reason string, ch Change, startRefs []domain.VersionRef) (Job, error) {
	id, err := m.st.NextID(ctx, colJobs)
	if err != nil {
		return Job{}, err
	}
	j := Job{ID: id, Status: StatusQueued, Reason: reason, Change: ch, StartRefs: startRefs, CreatedAt: m.st.Now()}
	if err := m.st.PutDoc(ctx, colJobs, id, j); err != nil {
		return j, err
	}
	m.mu.Lock()
	m.queued = append(m.queued, id)
	m.mu.Unlock()
	return j, nil
}

// RunAvailable executes one currently queued job. It pins the refs stored with
// the job; publications made during execution only create later queued jobs.
func (m *Manager) RunAvailable(ctx context.Context) (bool, error) {
	m.mu.Lock()
	if m.running || len(m.queued) == 0 {
		m.mu.Unlock()
		return false, nil
	}
	id := m.queued[0]
	m.queued = m.queued[1:]
	m.running = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.running = false; m.mu.Unlock() }()

	var j Job
	if err := m.st.GetDoc(ctx, colJobs, id, &j); err != nil {
		return false, err
	}
	now := m.st.Now()
	j.Status, j.StartedAt = StatusRunning, &now
	if err := m.st.PutDoc(ctx, colJobs, id, j); err != nil {
		return false, err
	}
	res, err := m.handler(ctx, j)
	finished := m.st.Now()
	j.FinishedAt = &finished
	if err != nil {
		j.Status, j.ErrorMessage = StatusFailed, err.Error()
	} else {
		j.Status, j.Result = StatusComplete, res
		j.Progress = j.Total
	}
	if err := m.st.PutDoc(ctx, colJobs, id, j); err != nil {
		return true, err
	}
	return true, err
}

func (m *Manager) Get(ctx context.Context, id string) (Job, error) {
	var j Job
	return j, m.st.GetDoc(ctx, colJobs, id, &j)
}

func (m *Manager) List(ctx context.Context) ([]Job, error) {
	var jobs []Job
	return jobs, m.st.ListDocs(ctx, colJobs, &jobs)
}

func (m *Manager) Save(ctx context.Context, j Job) error { return m.st.PutDoc(ctx, colJobs, j.ID, j) }

// StartWorker runs queued recomputation jobs until ctx is canceled. A job's
// version snapshot is fixed in its document, so polling does not make it pick
// up later publications; those get their own queued/follow-up jobs.
func (m *Manager) StartWorker(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = m.RunAvailable(ctx)
			}
		}
	}()
}
