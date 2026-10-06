package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"microwaveplanner/internal/model"
)

// SaveLinkEvaluation persists a hop evaluation.
func (s *Store) SaveLinkEvaluation(ctx context.Context, e model.LinkEvaluation) error {
	raw, err := jsonMarshal(e)
	if err != nil {
		return err
	}
	lv, err := jsonMarshal(e.LayerVersions)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO link_evaluations(id,link_id,kind,task_id,evaluated_at,layer_versions,input_hash,payload)
		 VALUES(?,?,?,?,?,?,?,?)`,
		e.ID, e.LinkID, e.Kind, e.TaskID, e.EvaluatedAt, lv, e.InputHash, raw)
	return err
}

// GetLinkEvaluation loads a historical hop evaluation including its full
// reproducibility snapshot.
func (s *Store) GetLinkEvaluation(ctx context.Context, id string) (*model.LinkEvaluation, error) {
	var e model.LinkEvaluation
	var raw, lv []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,link_id,kind,task_id,evaluated_at,layer_versions,input_hash,payload
		 FROM link_evaluations WHERE id=?`, id).
		Scan(&e.ID, &e.LinkID, &e.Kind, &e.TaskID, &e.EvaluatedAt, &lv, &e.InputHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: link evaluation %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	e, err = jsonUnmarshal[model.LinkEvaluation](raw)
	if err != nil {
		return nil, err
	}
	e.LayerVersions, err = jsonUnmarshal[map[string]any](lv)
	return &e, err
}

// ListLinkEvaluations lists a hop's history, newest first.
func (s *Store) ListLinkEvaluations(ctx context.Context, linkID string, limit int) ([]model.LinkEvaluation, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT payload FROM link_evaluations WHERE link_id=? ORDER BY evaluated_at DESC,id DESC LIMIT ?`, linkID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvaluationRows[model.LinkEvaluation](rows)
}

// LatestLinkEvaluation returns the newest stored hop assessment.
func (s *Store) LatestLinkEvaluation(ctx context.Context, linkID string) (*model.LinkEvaluation, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT payload FROM link_evaluations WHERE link_id=? ORDER BY evaluated_at DESC,id DESC LIMIT 1`, linkID).
		Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e, err := jsonUnmarshal[model.LinkEvaluation](raw)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// SaveChannelEvaluation persists a channel evaluation.
func (s *Store) SaveChannelEvaluation(ctx context.Context, e model.ChannelEvaluation) error {
	raw, err := jsonMarshal(e)
	if err != nil {
		return err
	}
	lv, err := jsonMarshal(e.LayerVersions)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO channel_evaluations(id,channel_id,kind,task_id,evaluated_at,layer_versions,input_hash,payload)
		 VALUES(?,?,?,?,?,?,?,?)`,
		e.ID, e.ChannelID, e.Kind, e.TaskID, e.EvaluatedAt, lv, e.InputHash, raw)
	return err
}

// GetChannelEvaluation loads a historical channel evaluation.
func (s *Store) GetChannelEvaluation(ctx context.Context, id string) (*model.ChannelEvaluation, error) {
	var e model.ChannelEvaluation
	var raw, lv []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,channel_id,kind,task_id,evaluated_at,layer_versions,input_hash,payload
		 FROM channel_evaluations WHERE id=?`, id).
		Scan(&e.ID, &e.ChannelID, &e.Kind, &e.TaskID, &e.EvaluatedAt, &lv, &e.InputHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: channel evaluation %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	e, err = jsonUnmarshal[model.ChannelEvaluation](raw)
	if err != nil {
		return nil, err
	}
	e.LayerVersions, err = jsonUnmarshal[map[string]any](lv)
	return &e, err
}

// ListChannelEvaluations lists a channel's history, newest first.
func (s *Store) ListChannelEvaluations(ctx context.Context, channelID string, limit int) ([]model.ChannelEvaluation, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT payload FROM channel_evaluations WHERE channel_id=? ORDER BY evaluated_at DESC,id DESC LIMIT ?`, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvaluationRows[model.ChannelEvaluation](rows)
}

// LatestChannelEvaluation returns the newest stored channel assessment.
func (s *Store) LatestChannelEvaluation(ctx context.Context, channelID string) (*model.ChannelEvaluation, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT payload FROM channel_evaluations WHERE channel_id=? ORDER BY evaluated_at DESC,id DESC LIMIT 1`, channelID).
		Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e, err := jsonUnmarshal[model.ChannelEvaluation](raw)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

type payloadRows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

func scanEvaluationRows[T any](rows *sql.Rows) ([]T, error) {
	var out []T
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		v, err := jsonUnmarshal[T](raw)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CreateTask inserts a queued task.
func (s *Store) CreateTask(ctx context.Context, t model.RecomputeTask) error {
	baseline, err := jsonMarshal(t.Baseline)
	if err != nil {
		return err
	}
	target, err := jsonMarshal(t.Target)
	if err != nil {
		return err
	}
	progress, err := jsonMarshal(t.Progress)
	if err != nil {
		return err
	}
	affected, err := jsonMarshal(t.Affected)
	if err != nil {
		return err
	}
	diff, err := jsonMarshal(t.Diff)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO recompute_tasks(id,status,reason,trigger_source,baseline,target,progress,submitted_at,
		 started_at,finished_at,error_text,affected,diff,followup_of)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Status, t.Reason, t.Trigger, baseline, target, progress, t.SubmittedAt,
		nullTimePtr(t.StartedAt), nullTimePtr(t.FinishedAt), t.Error, affected, diff, t.FollowupOf)
	return err
}

// GetTask loads a task.
func (s *Store) GetTask(ctx context.Context, id string) (*model.RecomputeTask, error) {
	var t model.RecomputeTask
	var baseline, target, progress, affected, diff []byte
	var started, finished sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,status,reason,trigger_source,baseline,target,progress,submitted_at,started_at,finished_at,
		 error_text,affected,diff,followup_of
		 FROM recompute_tasks WHERE id=?`, id).
		Scan(&t.ID, &t.Status, &t.Reason, &t.Trigger, &baseline, &target, &progress,
			&t.SubmittedAt, &started, &finished, &t.Error, &affected, &diff, &t.FollowupOf)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: task %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	if t.Baseline, err = jsonUnmarshal[model.LayerVersions](baseline); err != nil {
		return nil, err
	}
	if t.Target, err = jsonUnmarshal[model.LayerVersions](target); err != nil {
		return nil, err
	}
	if t.Progress, err = jsonUnmarshal[model.TaskProgress](progress); err != nil {
		return nil, err
	}
	if t.Affected, err = jsonUnmarshal[model.TaskAffected](affected); err != nil {
		return nil, err
	}
	if t.Diff, err = jsonUnmarshal[model.TaskDiff](diff); err != nil {
		return nil, err
	}
	t.StartedAt = timePtr(started)
	t.FinishedAt = timePtr(finished)
	return &t, nil
}

// UpdateTaskProgress persists progress.
func (s *Store) UpdateTaskProgress(ctx context.Context, id string, p model.TaskProgress) error {
	raw, err := jsonMarshal(p)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE recompute_tasks SET progress=? WHERE id=?`, raw, id)
	return err
}

// FinishTask marks completion and stores diff/error.
func (s *Store) FinishTask(ctx context.Context, t *model.RecomputeTask) error {
	now := time.Now().UTC()
	t.FinishedAt = &now
	progress, err := jsonMarshal(t.Progress)
	if err != nil {
		return err
	}
	affected, err := jsonMarshal(t.Affected)
	if err != nil {
		return err
	}
	diff, err := jsonMarshal(t.Diff)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE recompute_tasks SET status=?,progress=?,error_text=?,affected=?,diff=?,finished_at=? WHERE id=?`,
		t.Status, progress, t.Error, affected, diff, now, t.ID)
	return err
}

// MarkTaskRunning atomically claims a queued task.
func (s *Store) MarkTaskRunning(ctx context.Context, id string) (bool, error) {
	now := time.Now().UTC()
	res, err := s.DB.ExecContext(ctx,
		`UPDATE recompute_tasks SET status='running', started_at=? WHERE id=? AND status='queued'`,
		now, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// OldestQueuedTask returns the earliest queued task ID.
func (s *Store) OldestQueuedTask(ctx context.Context) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM recompute_tasks WHERE status='queued' ORDER BY submitted_at,id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// HasActiveTaskAfter reports whether another queued/running task already has a
// target snapshot different from the given frozen heads. It prevents the
// runner from stacking redundant follow-up tasks when several layers were
// published during execution and each publication already enqueued its own
// recompute task.
func (s *Store) HasActiveTaskAfter(ctx context.Context, frozen model.LayerVersions, excludeTaskID string) (bool, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT target FROM recompute_tasks
		 WHERE status IN ('queued','running') AND id <> ?`, excludeTaskID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		target, err := jsonUnmarshal[model.LayerVersions](raw)
		if err != nil {
			return false, err
		}
		if target != frozen {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ListTasks lists recent tasks.
func (s *Store) ListTasks(ctx context.Context, limit int) ([]model.RecomputeTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id FROM recompute_tasks ORDER BY submitted_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]model.RecomputeTask, 0, len(ids))
	for _, id := range ids {
		t, err := s.GetTask(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, nil
}
