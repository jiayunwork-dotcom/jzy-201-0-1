package store

import (
	"context"

	"microwaveplanner/internal/model"
	"microwaveplanner/internal/params"
)

// DeleteTaskSnapshots removes frozen rows for a task.
func (s *Store) DeleteTaskSnapshots(ctx context.Context, taskID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM task_endpoint_snapshots WHERE task_id=?`, taskID)
	return err
}

// CurrentEndpointSnapshot returns the head versions currently resolved by an
// endpoint. Zero means that optional scope has no published set.
func (s *Store) CurrentEndpointSnapshot(ctx context.Context, endpointID string) (model.Endpoint, model.EndpointSnapshot, error) {
	ep, err := s.GetEndpoint(ctx, endpointID)
	if err != nil {
		return model.Endpoint{}, model.EndpointSnapshot{}, err
	}
	snap := model.EndpointSnapshot{EndpointID: endpointID}
	if ps, ok, err := s.LatestSet(ctx, "nominal", "radio_model", ep.RadioModelID); err != nil {
		return *ep, snap, err
	} else if ok {
		snap.RadioNominalVersion = ps.Version
	}
	if ps, ok, err := s.LatestSet(ctx, "nominal", "antenna_model", ep.AntennaModelID); err != nil {
		return *ep, snap, err
	} else if ok {
		snap.AntennaNominalVersion = ps.Version
	}
	if ep.RadioBatchID != "" {
		if ps, ok, err := s.LatestSet(ctx, "batch", "radio_batch", ep.RadioBatchID); err != nil {
			return *ep, snap, err
		} else if ok {
			snap.RadioBatchVersion = ps.Version
		}
	}
	if ep.AntennaBatchID != "" {
		if ps, ok, err := s.LatestSet(ctx, "batch", "antenna_batch", ep.AntennaBatchID); err != nil {
			return *ep, snap, err
		} else if ok {
			snap.AntennaBatchVersion = ps.Version
		}
	}
	if ps, ok, err := s.LatestSet(ctx, "site", "endpoint_radio", ep.ID); err != nil {
		return *ep, snap, err
	} else if ok {
		snap.RadioSiteVersion = ps.Version
	}
	if ps, ok, err := s.LatestSet(ctx, "site", "endpoint_antenna", ep.ID); err != nil {
		return *ep, snap, err
	} else if ok {
		snap.AntennaSiteVersion = ps.Version
	}
	return *ep, snap, nil
}

// SaveTaskEndpointSnapshots freezes supplied endpoint references.
func (s *Store) SaveTaskEndpointSnapshots(ctx context.Context, taskID string, endpointIDs []string) error {
	if err := s.DeleteTaskSnapshots(ctx, taskID); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range endpointIDs {
		ep, snap, err := currentSnapshotTx(ctx, s, id)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO task_endpoint_snapshots(task_id,endpoint_id,radio_nominal_version,
			  radio_batch_version,radio_site_version,antenna_nominal_version,antenna_batch_version,antenna_site_version)
			 VALUES(?,?,?,?,?,?,?,?)`,
			taskID, id, snap.RadioNominalVersion, snap.RadioBatchVersion, snap.RadioSiteVersion,
			snap.AntennaNominalVersion, snap.AntennaBatchVersion, snap.AntennaSiteVersion)
		if err != nil {
			return err
		}
		_ = ep
	}
	return tx.Commit()
}

func currentSnapshotTx(ctx context.Context, s *Store, endpointID string) (model.Endpoint, model.EndpointSnapshot, error) {
	return s.CurrentEndpointSnapshot(ctx, endpointID)
}

// SnapshotEndpoint resolves one endpoint using exact frozen set versions.
func (s *Store) SnapshotEndpoint(ctx context.Context, ep model.Endpoint, snap model.EndpointSnapshot) (*model.EffectiveEndpointView, error) {
	radioNom, err := s.SetAtVersion(ctx, params.LayerNominal, "radio_model", ep.RadioModelID, snap.RadioNominalVersion)
	if err != nil {
		return nil, err
	}
	antNom, err := s.SetAtVersion(ctx, params.LayerNominal, "antenna_model", ep.AntennaModelID, snap.AntennaNominalVersion)
	if err != nil {
		return nil, err
	}
	var radioBatch, radioSite, antBatch, antSite *model.ParamSet
	if ep.RadioBatchID != "" && snap.RadioBatchVersion > 0 {
		radioBatch, err = s.SetAtVersion(ctx, params.LayerBatch, "radio_batch", ep.RadioBatchID, snap.RadioBatchVersion)
		if err != nil {
			return nil, err
		}
	}
	if ep.AntennaBatchID != "" && snap.AntennaBatchVersion > 0 {
		antBatch, err = s.SetAtVersion(ctx, params.LayerBatch, "antenna_batch", ep.AntennaBatchID, snap.AntennaBatchVersion)
		if err != nil {
			return nil, err
		}
	}
	if snap.RadioSiteVersion > 0 {
		radioSite, err = s.SetAtVersion(ctx, params.LayerSite, "endpoint_radio", ep.ID, snap.RadioSiteVersion)
		if err != nil {
			return nil, err
		}
	}
	if snap.AntennaSiteVersion > 0 {
		antSite, err = s.SetAtVersion(ctx, params.LayerSite, "endpoint_antenna", ep.ID, snap.AntennaSiteVersion)
		if err != nil {
			return nil, err
		}
	}
	resolved := params.ResolveEndpoint(params.EndpointInput{
		EndpointID:     ep.ID,
		RadioNominal:   toVersionedSet(radioNom),
		RadioBatch:     toVersionedSet(radioBatch),
		RadioSite:      toVersionedSet(radioSite),
		AntennaNominal: toVersionedSet(antNom),
		AntennaBatch:   toVersionedSet(antBatch),
		AntennaSite:    toVersionedSet(antSite),
	})
	return &model.EffectiveEndpointView{
		EffectiveEndpoint: resolved, SiteID: ep.SiteID,
		RadioModelID: ep.RadioModelID, RadioBatchID: ep.RadioBatchID,
		AntennaModelID: ep.AntennaModelID, AntennaBatchID: ep.AntennaBatchID,
	}, nil
}

// TaskSnapshots returns frozen endpoint versions for a task.
func (s *Store) TaskSnapshots(ctx context.Context, taskID string) ([]model.EndpointSnapshot, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT endpoint_id,radio_nominal_version,radio_batch_version,radio_site_version,
		        antenna_nominal_version,antenna_batch_version,antenna_site_version
		 FROM task_endpoint_snapshots WHERE task_id=? ORDER BY endpoint_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.EndpointSnapshot
	for rows.Next() {
		var x model.EndpointSnapshot
		if err := rows.Scan(&x.EndpointID, &x.RadioNominalVersion, &x.RadioBatchVersion,
			&x.RadioSiteVersion, &x.AntennaNominalVersion, &x.AntennaBatchVersion,
			&x.AntennaSiteVersion); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
