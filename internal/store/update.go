package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"microwaveplanner/internal/model"
	"microwaveplanner/internal/params"
)

// UpdateSite changes editable station attributes.
func (s *Store) UpdateSite(ctx context.Context, site model.Site) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE sites SET name=?, rain_region_id=?, remark=?, updated_at=? WHERE id=?`,
		site.Name, site.Region, site.Remark, time.Now().UTC(), site.ID)
	if err != nil {
		return err
	}
	return requireOneRow(res)
}

// UpdateEndpoint changes an endpoint's editable identity and model/batch
// references after validating that all references exist.
func (s *Store) UpdateEndpoint(ctx context.Context, ep model.Endpoint) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		var siteID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM sites WHERE id=? FOR UPDATE`, ep.SiteID).Scan(&siteID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := requireLatestExists(ctx, tx, params.LayerNominal, "radio_model", ep.RadioModelID, "radio model"); err != nil {
			return err
		}
		if err := requireLatestExists(ctx, tx, params.LayerNominal, "antenna_model", ep.AntennaModelID, "antenna model"); err != nil {
			return err
		}
		if ep.RadioBatchID != "" {
			if err := requireLatestExists(ctx, tx, params.LayerBatch, "radio_batch", ep.RadioBatchID, "radio batch"); err != nil {
				return err
			}
		}
		if ep.AntennaBatchID != "" {
			if err := requireLatestExists(ctx, tx, params.LayerBatch, "antenna_batch", ep.AntennaBatchID, "antenna batch"); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE endpoints SET site_id=?, name=?, radio_model_id=?, radio_batch_id=?,
			   antenna_model_id=?, antenna_batch_id=?, updated_at=? WHERE id=?`,
			ep.SiteID, ep.Name, ep.RadioModelID, ep.RadioBatchID,
			ep.AntennaModelID, ep.AntennaBatchID, time.Now().UTC(), ep.ID)
		if err != nil {
			return err
		}
		return requireOneRow(res)
	})
}

// UpdateLink changes editable hop attributes.
func (s *Store) UpdateLink(ctx context.Context, l model.Link) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE links SET name=?, end_a_id=?, end_b_id=?, site_a_id=?, site_b_id=?,
		   frequency_ghz=?, distance_km=?, polarization=?, rain_region_id=?,
		   rain_band_id=?, target_availability=?, updated_at=?
		 WHERE id=?`,
		l.Name, l.EndAID, l.EndBID, l.SiteAID, l.SiteBID, l.FrequencyGHz, l.DistanceKM,
		string(l.Polarization), l.RainRegionID, l.RainBandID, l.TargetAvailability,
		time.Now().UTC(), l.ID)
	if err != nil {
		return err
	}
	return requireOneRow(res)
}

// UpdateChannel replaces editable channel attributes and its ordered link list.
func (s *Store) UpdateChannel(ctx context.Context, ch model.Channel) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE channels SET name=?, target_availability=?, correlation_rho=?, updated_at=? WHERE id=?`,
			ch.Name, ch.TargetAvailability, ch.CorrelationRho, time.Now().UTC(), ch.ID)
		if err != nil {
			return err
		}
		if err := requireOneRow(res); err != nil {
			return err
		}
		return replaceChannelLinksTx(ctx, tx, ch)
	})
}

func requireOneRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
