package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"microwaveplanner/internal/model"
	"microwaveplanner/internal/params"
)

// ErrNoDraft indicates that publication was requested for a scope without a
// saved draft.
var ErrNoDraft = errors.New("no draft to publish for this layer/scope")

// lockedDraft reads a draft with a row lock.
func lockedDraft(ctx context.Context, tx *sql.Tx, layer params.Layer, scopeKind, scopeID string) (*model.ParamDraft, error) {
	var d model.ParamDraft
	var raw []byte
	err := tx.QueryRowContext(ctx,
		`SELECT fields, revision, updated_at, updated_by FROM param_drafts
		 WHERE layer=? AND scope_kind=? AND scope_id=? FOR UPDATE`,
		layer, scopeKind, scopeID).Scan(&raw, &d.Revision, &d.UpdatedAt, &d.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s/%s/%s", ErrNoDraft, layer, scopeKind, scopeID)
	}
	if err != nil {
		return nil, err
	}
	d.Layer = layer
	d.ScopeKind = scopeKind
	d.ScopeID = scopeID
	d.Fields, err = jsonUnmarshal[map[string]float64](raw)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// endpointSetRefs identifies the six parameter scopes resolved by one end.
type endpointSetRefs struct {
	Endpoint model.Endpoint
}

func matchingEndpointsTx(ctx context.Context, tx *sql.Tx, layer params.Layer, scopeKind, scopeID string) ([]model.Endpoint, error) {
	var rs *sql.Rows
	var err error
	const q = `SELECT id,site_id,name,radio_model_id,radio_batch_id,antenna_model_id,antenna_batch_id FROM endpoints`
	switch {
	case layer == params.LayerNominal && scopeKind == "radio_model":
		rs, err = tx.QueryContext(ctx, q+` WHERE radio_model_id=? ORDER BY id`, scopeID)
	case layer == params.LayerNominal && scopeKind == "antenna_model":
		rs, err = tx.QueryContext(ctx, q+` WHERE antenna_model_id=? ORDER BY id`, scopeID)
	case layer == params.LayerBatch && scopeKind == "radio_batch":
		rs, err = tx.QueryContext(ctx, q+` WHERE radio_batch_id=? ORDER BY id`, scopeID)
	case layer == params.LayerBatch && scopeKind == "antenna_batch":
		rs, err = tx.QueryContext(ctx, q+` WHERE antenna_batch_id=? ORDER BY id`, scopeID)
	case layer == params.LayerSite && (scopeKind == "endpoint_radio" || scopeKind == "endpoint_antenna"):
		rs, err = tx.QueryContext(ctx, q+` WHERE id=? ORDER BY id`, scopeID)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []model.Endpoint
	for rs.Next() {
		ep, err := scanEndpoint(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, *ep)
	}
	return out, rs.Err()
}

// latestSetTx6 loads the six currently published sets for one endpoint.
func latestSetsForEndpoint(ctx context.Context, q queryRower, ep model.Endpoint) (map[string]*model.ParamSet, error) {
	load := func(key, kind, id string, optional bool) (*model.ParamSet, error) {
		if id == "" {
			return nil, nil
		}
		ps, ok, err := latestSetTx(ctx, q, layerOfKind(kind), kind, id)
		if err != nil {
			return nil, err
		}
		if !ok && !optional {
			return nil, fmt.Errorf("%w: %s %s", ErrNotFound, kind, id)
		}
		if !ok {
			return nil, nil
		}
		return ps, nil
	}
	out := map[string]*model.ParamSet{}
	var first error
	add := func(key, kind, id string, optional bool) {
		if first != nil {
			return
		}
		ps, err := load(key, kind, id, optional)
		if err != nil {
			first = err
			return
		}
		out[key] = ps
	}
	add("radioNominal", "radio_model", ep.RadioModelID, false)
	add("antennaNominal", "antenna_model", ep.AntennaModelID, false)
	add("radioBatch", "radio_batch", ep.RadioBatchID, true)
	add("antennaBatch", "antenna_batch", ep.AntennaBatchID, true)
	add("radioSite", "endpoint_radio", ep.ID, true)
	add("antennaSite", "endpoint_antenna", ep.ID, true)
	if first != nil {
		return nil, first
	}
	return out, nil
}

func layerOfKind(kind string) params.Layer {
	switch kind {
	case "radio_model", "antenna_model":
		return params.LayerNominal
	case "radio_batch", "antenna_batch":
		return params.LayerBatch
	default:
		return params.LayerSite
	}
}

func resolveSetMap(ep model.Endpoint, sets map[string]*model.ParamSet) *params.EffectiveEndpoint {
	return params.ResolveEndpoint(params.EndpointInput{
		EndpointID:     ep.ID,
		RadioNominal:   toVersionedSet(sets["radioNominal"]),
		RadioBatch:     toVersionedSet(sets["radioBatch"]),
		RadioSite:      toVersionedSet(sets["radioSite"]),
		AntennaNominal: toVersionedSet(sets["antennaNominal"]),
		AntennaBatch:   toVersionedSet(sets["antennaBatch"]),
		AntennaSite:    toVersionedSet(sets["antennaSite"]),
	})
}

func snapshotFromSets(ep model.Endpoint, sets map[string]*model.ParamSet) model.EndpointSnapshot {
	version := func(key string) int64 {
		if ps := sets[key]; ps != nil {
			return ps.Version
		}
		return 0
	}
	return model.EndpointSnapshot{
		EndpointID:            ep.ID,
		RadioNominalVersion:   version("radioNominal"),
		RadioBatchVersion:     version("radioBatch"),
		RadioSiteVersion:      version("radioSite"),
		AntennaNominalVersion: version("antennaNominal"),
		AntennaBatchVersion:   version("antennaBatch"),
		AntennaSiteVersion:    version("antennaSite"),
	}
}

func linksForEndpointsTx(ctx context.Context, tx *sql.Tx, endpointIDs []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, ep := range endpointIDs {
		rows, err := tx.QueryContext(ctx,
			`SELECT id FROM links WHERE end_a_id=? OR end_b_id=? ORDER BY id`, ep, ep)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				out = append(out, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func channelsForLinksTx(ctx context.Context, tx *sql.Tx, linkIDs []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, linkID := range linkIDs {
		rows, err := tx.QueryContext(ctx,
			`SELECT DISTINCT channel_id FROM channel_links WHERE link_id=? ORDER BY channel_id`, linkID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				out = append(out, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func linksForChannelTx(ctx context.Context, tx *sql.Tx, channelID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT link_id FROM channel_links WHERE channel_id=? ORDER BY position`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func endpointIDsForLinksTx(ctx context.Context, tx *sql.Tx, linkIDs []string) ([]model.Endpoint, error) {
	seen := map[string]struct{}{}
	var ids []string
	for _, linkID := range linkIDs {
		var a, b string
		err := tx.QueryRowContext(ctx, `SELECT end_a_id, end_b_id FROM links WHERE id=?`, linkID).Scan(&a, &b)
		if err != nil {
			return nil, err
		}
		for _, id := range []string{a, b} {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]model.Endpoint, 0, len(ids))
	for _, id := range ids {
		ep, err := getEndpointTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *ep)
	}
	return out, nil
}

func getEndpointTx(ctx context.Context, q queryRower, id string) (*model.Endpoint, error) {
	row := q.QueryRowContext(ctx, endpointSelect+` WHERE id=?`, id)
	return scanEndpoint(row)
}

func insertTaskTx(ctx context.Context, tx *sql.Tx, t *model.RecomputeTask) error {
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
	_, err = tx.ExecContext(ctx,
		`INSERT INTO recompute_tasks(id,status,reason,trigger_source,baseline,target,progress,submitted_at,
		 started_at,finished_at,error_text,affected,diff,followup_of)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Status, t.Reason, t.Trigger, baseline, target, progress, t.SubmittedAt,
		nil, nil, t.Error, affected, diff, t.FollowupOf)
	return err
}

func insertSnapshotsTx(ctx context.Context, tx *sql.Tx, taskID string, eps []model.Endpoint, snapshotted map[string]model.EndpointSnapshot) error {
	for _, ep := range eps {
		snap := snapshotted[ep.ID]
		_, err := tx.ExecContext(ctx,
			`INSERT INTO task_endpoint_snapshots(task_id,endpoint_id,radio_nominal_version,
			  radio_batch_version,radio_site_version,antenna_nominal_version,antenna_batch_version,antenna_site_version)
			 VALUES(?,?,?,?,?,?,?,?)`,
			taskID, ep.ID, snap.RadioNominalVersion, snap.RadioBatchVersion, snap.RadioSiteVersion,
			snap.AntennaNominalVersion, snap.AntennaBatchVersion, snap.AntennaSiteVersion)
		if err != nil {
			return err
		}
	}
	return nil
}

// PublishWithTask performs, in one transaction: draft publication, exact
// effective-parameter impact analysis, queued recompute-task creation and
// freezing of every affected endpoint's six set versions. Publication and the
// frozen versions are therefore atomic and always consistent.
func (s *Store) PublishWithTask(ctx context.Context, layer params.Layer, scopeKind, scopeID, user, comment string) (*PublishResult, *model.RecomputeTask, error) {
	var pub PublishResult
	var task *model.RecomputeTask
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

		matched, err := matchingEndpointsTx(ctx, tx, layer, scopeKind, scopeID)
		if err != nil {
			return err
		}
		before := map[string]string{}
		beforeSets := map[string]map[string]*model.ParamSet{}
		for _, ep := range matched {
			sets, err := latestSetsForEndpoint(ctx, tx, ep)
			if err != nil {
				return err
			}
			before[ep.ID] = resolveSetMap(ep, sets).NumericHash()
			beforeSets[ep.ID] = sets
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
			id, layer, scopeKind, scopeID, next, raw, now, user, comment)
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
		pub.Set = model.ParamSet{
			ID: id, Layer: layer, ScopeKind: scopeKind, ScopeID: scopeID,
			Version: next, Fields: draft.Fields, PublishedAt: now,
			PublishedBy: user, Comment: comment,
		}
		pub.HadPrevious = hasPrev
		pub.HeadBefore = head
		pub.HeadAfter = next
		if hasPrev {
			pub.Previous = *prev
		}

		heads, err := allHeadsTx(ctx, tx)
		if err != nil {
			return err
		}
		baseline := heads
		switch layer {
		case params.LayerNominal:
			baseline.Nominal = head
		case params.LayerBatch:
			baseline.Batch = head
		case params.LayerSite:
			baseline.Site = head
		}

		changedEndpoints := map[string]struct{}{}
		snapshots := map[string]model.EndpointSnapshot{}
		for _, ep := range matched {
			sets, err := latestSetsForEndpoint(ctx, tx, ep)
			if err != nil {
				return err
			}
			afterHash := resolveSetMap(ep, sets).NumericHash()
			if before[ep.ID] != afterHash {
				changedEndpoints[ep.ID] = struct{}{}
				snapshots[ep.ID] = snapshotFromSets(ep, sets)
			}
		}
		var epIDs []string
		for id := range changedEndpoints {
			epIDs = append(epIDs, id)
		}
		sort.Strings(epIDs)
		links, err := linksForEndpointsTx(ctx, tx, epIDs)
		if err != nil {
			return err
		}
		channels, err := channelsForLinksTx(ctx, tx, links)
		if err != nil {
			return err
		}

		// Freeze every endpoint the task will actually evaluate: both ends of
		// every affected link, and both ends of every link that belongs to an
		// affected channel (channel availability is computed for the whole
		// tandem, including hops whose own parameters did not change).
		linksToFreeze := append([]string{}, links...)
		channelLinkSeen := map[string]struct{}{}
		for _, l := range links {
			channelLinkSeen[l] = struct{}{}
		}
		for _, chID := range channels {
			memberLinks, err := linksForChannelTx(ctx, tx, chID)
			if err != nil {
				return err
			}
			for _, l := range memberLinks {
				if _, ok := channelLinkSeen[l]; ok {
					continue
				}
				channelLinkSeen[l] = struct{}{}
				linksToFreeze = append(linksToFreeze, l)
			}
		}
		sort.Strings(linksToFreeze)
		linkEndpoints, err := endpointIDsForLinksTx(ctx, tx, linksToFreeze)
		if err != nil {
			return err
		}
		for _, ep := range linkEndpoints {
			if _, ok := snapshots[ep.ID]; ok {
				continue
			}
			sets, err := latestSetsForEndpoint(ctx, tx, ep)
			if err != nil {
				return err
			}
			snapshots[ep.ID] = snapshotFromSets(ep, sets)
		}

		task = &model.RecomputeTask{
			ID: NewID("task"), Status: StatusQueuedStorage,
			Reason:  "automatic recomputation after publication of " + string(layer) + "/" + scopeKind + "/" + scopeID,
			Trigger: "publish", Baseline: baseline, Target: heads,
			SubmittedAt: now,
			Affected:    model.TaskAffected{Endpoints: epIDs, Links: links, Channels: channels},
		}
		if err := insertTaskTx(ctx, tx, task); err != nil {
			return err
		}
		return insertSnapshotsTx(ctx, tx, task.ID, linkEndpoints, snapshots)
	})
	if err != nil {
		return nil, nil, err
	}
	return &pub, task, nil
}

// StatusQueuedStorage mirrors task.StatusQueued without creating an import
// cycle (store must not import task).
const StatusQueuedStorage = "queued"

func allHeadsTx(ctx context.Context, tx *sql.Tx) (model.LayerVersions, error) {
	var out model.LayerVersions
	rows, err := tx.QueryContext(ctx, `SELECT layer, version FROM layer_heads`)
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

// CreateManualTask enqueues a full recompute and atomically freezes every
// endpoint's current set versions.
func (s *Store) CreateManualTask(ctx context.Context, reason, followupOf string) (*model.RecomputeTask, error) {
	t := &model.RecomputeTask{}
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		heads, err := allHeadsTx(ctx, tx)
		if err != nil {
			return err
		}
		eps, err := listEndpointsTx(ctx, tx)
		if err != nil {
			return err
		}
		links, err := listLinksTx(ctx, tx)
		if err != nil {
			return err
		}
		channels, err := listChannelsTx(ctx, tx)
		if err != nil {
			return err
		}
		epIDs := make([]string, len(eps))
		linkIDs := make([]string, len(links))
		chIDs := make([]string, len(channels))
		for i, ep := range eps {
			epIDs[i] = ep.ID
		}
		for i, l := range links {
			linkIDs[i] = l.ID
		}
		for i, ch := range channels {
			chIDs[i] = ch.ID
		}
		sort.Strings(epIDs)
		sort.Strings(linkIDs)
		sort.Strings(chIDs)
		now := time.Now().UTC()
		t = &model.RecomputeTask{
			ID: NewID("task"), Status: StatusQueuedStorage, Reason: reason,
			Trigger: "manual", Baseline: heads, Target: heads, SubmittedAt: now,
			Affected:   model.TaskAffected{Endpoints: epIDs, Links: linkIDs, Channels: chIDs},
			FollowupOf: followupOf,
		}
		if err := insertTaskTx(ctx, tx, t); err != nil {
			return err
		}
		snapshots := map[string]model.EndpointSnapshot{}
		for _, ep := range eps {
			sets, err := latestSetsForEndpoint(ctx, tx, ep)
			if err != nil {
				return err
			}
			snapshots[ep.ID] = snapshotFromSets(ep, sets)
		}
		return insertSnapshotsTx(ctx, tx, t.ID, eps, snapshots)
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// SetTaskFollowup annotates a follow-up task.
func (s *Store) SetTaskFollowup(ctx context.Context, taskID, parentID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE recompute_tasks SET followup_of=? WHERE id=?`, parentID, taskID)
	return err
}

func listEndpointsTx(ctx context.Context, tx *sql.Tx) ([]model.Endpoint, error) {
	rs, err := tx.QueryContext(ctx, endpointSelect+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []model.Endpoint
	for rs.Next() {
		ep, err := scanEndpoint(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, *ep)
	}
	return out, rs.Err()
}

func listLinksTx(ctx context.Context, tx *sql.Tx) ([]model.Link, error) {
	rs, err := tx.QueryContext(ctx, linkSelect+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []model.Link
	for rs.Next() {
		l, err := scanLink(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rs.Err()
}

func listChannelsTx(ctx context.Context, tx *sql.Tx) ([]model.Channel, error) {
	rs, err := tx.QueryContext(ctx, `SELECT id FROM channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []model.Channel
	for rs.Next() {
		var ch model.Channel
		if err := rs.Scan(&ch.ID); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rs.Err()
}
