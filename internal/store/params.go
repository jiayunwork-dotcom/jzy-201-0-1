package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"microwaveplanner/internal/model"
	"microwaveplanner/internal/params"
	"microwaveplanner/internal/version"
)

// ErrNotFound is returned for missing rows.
var ErrNotFound = errors.New("not found")

// ErrConflict is a data-level optimistic concurrency conflict.
var ErrConflict = errors.New("draft revision conflict")

// LayerHeads returns current global versions for all layers.
func (s *Store) LayerHeads(ctx context.Context) (model.LayerVersions, error) {
	var out model.LayerVersions
	rows, err := s.DB.QueryContext(ctx, `SELECT layer, version FROM layer_heads`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var layer string
		var v int64
		if err := rows.Scan(&layer, &v); err != nil {
			return out, err
		}
		switch params.Layer(layer) {
		case params.LayerNominal:
			out.Nominal = v
		case params.LayerBatch:
			out.Batch = v
		case params.LayerSite:
			out.Site = v
		}
	}
	return out, rows.Err()
}

// headVersionTx reads one head.
func headVersionTx(tx *sql.Tx, ctx context.Context, layer params.Layer) (int64, error) {
	var v int64
	err := tx.QueryRowContext(ctx, `SELECT version FROM layer_heads WHERE layer = ? FOR UPDATE`, layer).Scan(&v)
	return v, err
}

func bumpHeadTx(tx *sql.Tx, ctx context.Context, layer params.Layer, next int64) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE layer_heads SET version = ?, updated_at = ? WHERE layer = ?`,
		next, time.Now().UTC(), layer)
	return err
}

// GetDraft reads a draft. Missing drafts are not an error: revision zero and an
// empty field map represent the initial editable copy.
func (s *Store) GetDraft(ctx context.Context, layer params.Layer, scopeKind, scopeID string) (*model.ParamDraft, error) {
	return getDraft(ctx, s.DB, layer, scopeKind, scopeID)
}

func getDraft(ctx context.Context, q queryRower, layer params.Layer, scopeKind, scopeID string) (*model.ParamDraft, error) {
	var d model.ParamDraft
	var raw []byte
	d.Layer = layer
	d.ScopeKind = scopeKind
	d.ScopeID = scopeID
	err := q.QueryRowContext(ctx,
		`SELECT fields, revision, updated_at, updated_by FROM param_drafts
		 WHERE layer=? AND scope_kind=? AND scope_id=?`,
		layer, scopeKind, scopeID).Scan(&raw, &d.Revision, &d.UpdatedAt, &d.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		d.Fields = map[string]float64{}
		return &d, nil
	}
	if err != nil {
		return nil, err
	}
	d.Fields, err = jsonUnmarshal[map[string]float64](raw)
	if err != nil {
		return nil, err
	}
	if d.Fields == nil {
		d.Fields = map[string]float64{}
	}
	return &d, nil
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// PutDraftRequest is an optimistic draft write.
type PutDraftRequest struct {
	Layer        params.Layer
	ScopeKind    string
	ScopeID      string
	Fields       map[string]float64
	BaseRevision int64
	UpdatedBy    string
}

// PutDraft conditionally replaces draft fields. BaseRevision must equal the
// current draft revision (zero for the first edit).
func (s *Store) PutDraft(ctx context.Context, req PutDraftRequest) (*model.ParamDraft, error) {
	var out *model.ParamDraft
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		current, err := getDraft(ctx, tx, req.Layer, req.ScopeKind, req.ScopeID)
		if err != nil {
			return err
		}
		if err := version.CheckRevision(req.BaseRevision, current.Revision); err != nil {
			return err
		}
		nextRevision := current.Revision + 1
		now := time.Now().UTC()
		fields := req.Fields
		if fields == nil {
			fields = map[string]float64{}
		}
		raw, err := jsonMarshal(fields)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO param_drafts(layer, scope_kind, scope_id, fields, revision, updated_at, updated_by)
			 VALUES(?,?,?,?,?,?,?)
			 ON DUPLICATE KEY UPDATE fields=VALUES(fields), revision=VALUES(revision),
			 updated_at=VALUES(updated_at), updated_by=VALUES(updated_by)`,
			req.Layer, req.ScopeKind, req.ScopeID, raw, nextRevision, now, req.UpdatedBy)
		if err != nil {
			return err
		}
		out = &model.ParamDraft{
			Layer: req.Layer, ScopeKind: req.ScopeKind, ScopeID: req.ScopeID,
			Fields: fields, Revision: nextRevision, UpdatedAt: now, UpdatedBy: req.UpdatedBy,
		}
		return nil
	})
	var conflict *version.Conflict
	if errors.As(err, &conflict) {
		conflict.Layer = string(req.Layer)
		conflict.ScopeID = req.ScopeID
		return nil, conflict
	}
	return out, err
}

// DeleteDraft removes an unpublished draft if at the supplied revision.
func (s *Store) DeleteDraft(ctx context.Context, layer params.Layer, scopeKind, scopeID string, revision int64) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM param_drafts WHERE layer=? AND scope_kind=? AND scope_id=? AND revision=?`,
		layer, scopeKind, scopeID, revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

// PublishDraft atomically converts a draft to an immutable set and advances the
// global version of that layer. The returned set and prior fields are used by
// impact analysis.
type PublishResult struct {
	Set         model.ParamSet
	Previous    model.ParamSet
	HadPrevious bool
	HeadBefore  int64
	HeadAfter   int64
}

func (s *Store) PublishDraft(ctx context.Context, layer params.Layer, scopeKind, scopeID, publishedBy, comment string) (*PublishResult, error) {
	var result PublishResult
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		draft, err := lockedDraft(ctx, tx, layer, scopeKind, scopeID)
		if err != nil {
			return err
		}
		head, err := headVersionTx(tx, ctx, layer)
		if err != nil {
			return err
		}
		prev, hasPrev, err := latestSetTx(ctx, tx, layer, scopeKind, scopeID)
		if err != nil {
			return err
		}
		next := head + 1
		now := time.Now().UTC()
		id := NewID("ps")
		raw, err := jsonMarshal(draft.Fields)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO param_sets(id, layer, scope_kind, scope_id, version, fields, published_at, published_by, comment)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			id, layer, scopeKind, scopeID, next, raw, now, publishedBy, comment)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`DELETE FROM param_drafts WHERE layer=? AND scope_kind=? AND scope_id=?`,
			layer, scopeKind, scopeID)
		if err != nil {
			return err
		}
		if err := bumpHeadTx(tx, ctx, layer, next); err != nil {
			return err
		}
		set := model.ParamSet{
			ID: id, Layer: layer, ScopeKind: scopeKind, ScopeID: scopeID,
			Version: next, Fields: draft.Fields, PublishedAt: now,
			PublishedBy: publishedBy, Comment: comment,
		}
		result.Set = set
		result.HadPrevious = hasPrev
		result.HeadBefore = head
		result.HeadAfter = next
		if hasPrev {
			result.Previous = *prev
		}
		return nil
	})
	return &result, err
}

// LatestSet returns the newest published set in a scope.
func (s *Store) LatestSet(ctx context.Context, layer params.Layer, scopeKind, scopeID string) (*model.ParamSet, bool, error) {
	return latestSetTx(ctx, s.DB, layer, scopeKind, scopeID)
}

func latestSetTx(ctx context.Context, q queryRower, layer params.Layer, scopeKind, scopeID string) (*model.ParamSet, bool, error) {
	var ps model.ParamSet
	var raw []byte
	err := q.QueryRowContext(ctx,
		`SELECT id, layer, scope_kind, scope_id, version, fields, published_at, published_by, comment
		 FROM param_sets WHERE layer=? AND scope_kind=? AND scope_id=?
		 ORDER BY version DESC LIMIT 1`,
		layer, scopeKind, scopeID).Scan(
		&ps.ID, &ps.Layer, &ps.ScopeKind, &ps.ScopeID, &ps.Version, &raw,
		&ps.PublishedAt, &ps.PublishedBy, &ps.Comment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	ps.Fields, err = jsonUnmarshal[map[string]float64](raw)
	if err != nil {
		return nil, false, err
	}
	return &ps, true, nil
}

// SetAtVersion returns a specific immutable set version.
func (s *Store) SetAtVersion(ctx context.Context, layer params.Layer, scopeKind, scopeID string, version int64) (*model.ParamSet, error) {
	var ps model.ParamSet
	var raw []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, layer, scope_kind, scope_id, version, fields, published_at, published_by, comment
		 FROM param_sets WHERE layer=? AND scope_kind=? AND scope_id=? AND version=?`,
		layer, scopeKind, scopeID, version).Scan(
		&ps.ID, &ps.Layer, &ps.ScopeKind, &ps.ScopeID, &ps.Version, &raw,
		&ps.PublishedAt, &ps.PublishedBy, &ps.Comment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s/%s/%s version %d", ErrNotFound, layer, scopeKind, scopeID, version)
	}
	if err != nil {
		return nil, err
	}
	ps.Fields, err = jsonUnmarshal[map[string]float64](raw)
	return &ps, err
}

func toVersionedSet(ps *model.ParamSet) *params.VersionedSet {
	if ps == nil {
		return nil
	}
	return &params.VersionedSet{
		Ref:   params.Ref{Layer: ps.Layer, Version: ps.Version},
		SetID: ps.ID, Fields: ps.Fields,
	}
}
