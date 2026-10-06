package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/model"
	"microwaveplanner/internal/params"
)

// CreateSite inserts a station.
func (s *Store) CreateSite(ctx context.Context, site model.Site) error {
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sites(id,name,rain_region_id,remark,created_at,updated_at)
		 VALUES(?,?,?,?,?,?)`,
		site.ID, site.Name, site.Region, site.Remark, now, now)
	return err
}

// GetSite loads a station.
func (s *Store) GetSite(ctx context.Context, id string) (*model.Site, error) {
	var x model.Site
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,name,rain_region_id,remark FROM sites WHERE id=?`, id).
		Scan(&x.ID, &x.Name, &x.Region, &x.Remark)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: site %s", ErrNotFound, id)
	}
	return &x, err
}

// ListSites lists stations.
func (s *Store) ListSites(ctx context.Context) ([]model.Site, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,rain_region_id,remark FROM sites ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Site
	for rows.Next() {
		var x model.Site
		if err := rows.Scan(&x.ID, &x.Name, &x.Region, &x.Remark); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// CreateRainClimate inserts a climate region.
func (s *Store) CreateRainClimate(ctx context.Context, x model.RainClimate) error {
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO rain_climates(id,name,r001,created_at,updated_at) VALUES(?,?,?,?,?)`,
		x.ID, x.Name, x.R001, now, now)
	return err
}

// GetRainClimate loads a climate region.
func (s *Store) GetRainClimate(ctx context.Context, id string) (*model.RainClimate, error) {
	var x model.RainClimate
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,r001 FROM rain_climates WHERE id=?`, id).
		Scan(&x.ID, &x.Name, &x.R001)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: rain climate %s", ErrNotFound, id)
	}
	return &x, err
}

// CreateEndpoint inserts one end after verifying referenced models exist.
func (s *Store) CreateEndpoint(ctx context.Context, ep model.Endpoint) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := s.GetSite(ctx, ep.SiteID); err != nil {
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
		now := time.Now().UTC()
		_, err := tx.ExecContext(ctx,
			`INSERT INTO endpoints(id,site_id,name,radio_model_id,radio_batch_id,antenna_model_id,antenna_batch_id,created_at,updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			ep.ID, ep.SiteID, ep.Name, ep.RadioModelID, ep.RadioBatchID,
			ep.AntennaModelID, ep.AntennaBatchID, now, now)
		return err
	})
}

func requireLatestExists(ctx context.Context, q queryRower, layer params.Layer, kind, id, label string) error {
	var found string
	err := q.QueryRowContext(ctx,
		`SELECT scope_id FROM param_sets WHERE layer=? AND scope_kind=? AND scope_id=? LIMIT 1`,
		layer, kind, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: referenced %s %s", ErrNotFound, label, id)
	}
	return err
}

// GetEndpoint loads one end.
func (s *Store) GetEndpoint(ctx context.Context, id string) (*model.Endpoint, error) {
	return scanEndpoint(s.DB.QueryRowContext(ctx, endpointSelect+` WHERE id=?`, id))
}

// ListEndpoints lists all ends.
func (s *Store) ListEndpoints(ctx context.Context) ([]model.Endpoint, error) {
	rows, err := s.DB.QueryContext(ctx, endpointSelect+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Endpoint
	for rows.Next() {
		ep, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ep)
	}
	return out, rows.Err()
}

const endpointSelect = `SELECT id,site_id,name,radio_model_id,radio_batch_id,antenna_model_id,antenna_batch_id FROM endpoints`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEndpoint(sc rowScanner) (*model.Endpoint, error) {
	var ep model.Endpoint
	var radioBatch, antennaBatch sql.NullString
	err := sc.Scan(&ep.ID, &ep.SiteID, &ep.Name, &ep.RadioModelID, &radioBatch,
		&ep.AntennaModelID, &antennaBatch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: endpoint", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	ep.RadioBatchID = radioBatch.String
	ep.AntennaBatchID = antennaBatch.String
	return &ep, nil
}

// ResolveEndpoint loads and merges current published parameters for one end.
func (s *Store) ResolveEndpoint(ctx context.Context, endpointID string) (*model.EffectiveEndpointView, *model.Endpoint, error) {
	ep, err := s.GetEndpoint(ctx, endpointID)
	if err != nil {
		return nil, nil, err
	}
	v, err := s.resolveEndpointModel(ctx, ep)
	return v, ep, err
}

func (s *Store) resolveEndpointModel(ctx context.Context, ep *model.Endpoint) (*model.EffectiveEndpointView, error) {
	radioNom, ok, err := s.LatestSet(ctx, params.LayerNominal, "radio_model", ep.RadioModelID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: radio model %s", ErrNotFound, ep.RadioModelID)
	}
	antNom, ok, err := s.LatestSet(ctx, params.LayerNominal, "antenna_model", ep.AntennaModelID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: antenna model %s", ErrNotFound, ep.AntennaModelID)
	}
	var radioBatch, radioSite, antBatch, antSite *model.ParamSet
	if ep.RadioBatchID != "" {
		radioBatch, ok, err = s.LatestSet(ctx, params.LayerBatch, "radio_batch", ep.RadioBatchID)
		if err != nil || !ok {
			return nil, fmt.Errorf("%w: radio batch %s", ErrNotFound, ep.RadioBatchID)
		}
	}
	if ep.AntennaBatchID != "" {
		antBatch, ok, err = s.LatestSet(ctx, params.LayerBatch, "antenna_batch", ep.AntennaBatchID)
		if err != nil || !ok {
			return nil, fmt.Errorf("%w: antenna batch %s", ErrNotFound, ep.AntennaBatchID)
		}
	}
	radioSite, ok, err = s.LatestSet(ctx, params.LayerSite, "endpoint_radio", ep.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		radioSite = nil
	}
	antSite, ok, err = s.LatestSet(ctx, params.LayerSite, "endpoint_antenna", ep.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		antSite = nil
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
		EffectiveEndpoint: resolved,
		SiteID:            ep.SiteID, RadioModelID: ep.RadioModelID, RadioBatchID: ep.RadioBatchID,
		AntennaModelID: ep.AntennaModelID, AntennaBatchID: ep.AntennaBatchID,
	}, nil
}

// CreateLink inserts a hop.
func (s *Store) CreateLink(ctx context.Context, l model.Link) error {
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO links(id,name,end_a_id,end_b_id,site_a_id,site_b_id,frequency_ghz,distance_km,
		  polarization,rain_region_id,rain_band_id,target_availability,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.ID, l.Name, l.EndAID, l.EndBID, l.SiteAID, l.SiteBID, l.FrequencyGHz, l.DistanceKM,
		string(l.Polarization), l.RainRegionID, l.RainBandID, l.TargetAvailability, now, now)
	return err
}

// GetLink loads a hop.
func (s *Store) GetLink(ctx context.Context, id string) (*model.Link, error) {
	return scanLink(s.DB.QueryRowContext(ctx, linkSelect+` WHERE id=?`, id))
}

// ListLinks lists hops.
func (s *Store) ListLinks(ctx context.Context) ([]model.Link, error) {
	rows, err := s.DB.QueryContext(ctx, linkSelect+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Link
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

const linkSelect = `SELECT id,name,end_a_id,end_b_id,site_a_id,site_b_id,frequency_ghz,distance_km,
	polarization,rain_region_id,rain_band_id,target_availability FROM links`

func scanLink(sc rowScanner) (*model.Link, error) {
	var l model.Link
	var pol, region, band sql.NullString
	err := sc.Scan(&l.ID, &l.Name, &l.EndAID, &l.EndBID, &l.SiteAID, &l.SiteBID,
		&l.FrequencyGHz, &l.DistanceKM, &pol, &region, &band, &l.TargetAvailability)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: link", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	l.Polarization = linkbudget.Polarization(pol.String)
	l.RainRegionID = region.String
	l.RainBandID = band.String
	return &l, nil
}

// CreateChannel inserts a tandem channel and its ordered links.
func (s *Store) CreateChannel(ctx context.Context, ch model.Channel) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()
		_, err := tx.ExecContext(ctx,
			`INSERT INTO channels(id,name,target_availability,correlation_rho,created_at,updated_at)
			 VALUES(?,?,?,?,?,?)`, ch.ID, ch.Name, ch.TargetAvailability, ch.CorrelationRho, now, now)
		if err != nil {
			return err
		}
		return replaceChannelLinksTx(ctx, tx, ch)
	})
}

func replaceChannelLinksTx(ctx context.Context, tx *sql.Tx, ch model.Channel) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_links WHERE channel_id=?`, ch.ID); err != nil {
		return err
	}
	for i, linkID := range ch.LinkIDs {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO channel_links(channel_id,position,link_id) VALUES(?,?,?)`,
			ch.ID, i, linkID)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetChannel loads a channel and its ordered link IDs.
func (s *Store) GetChannel(ctx context.Context, id string) (*model.Channel, error) {
	var ch model.Channel
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,name,target_availability,correlation_rho FROM channels WHERE id=?`, id).
		Scan(&ch.ID, &ch.Name, &ch.TargetAvailability, &ch.CorrelationRho)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: channel %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT link_id FROM channel_links WHERE channel_id=? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var linkID string
		if err := rows.Scan(&linkID); err != nil {
			return nil, err
		}
		ch.LinkIDs = append(ch.LinkIDs, linkID)
	}
	return &ch, rows.Err()
}

// ListChannels lists channels.
func (s *Store) ListChannels(ctx context.Context) ([]model.Channel, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,target_availability,correlation_rho FROM channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Channel
	for rows.Next() {
		var ch model.Channel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.TargetAvailability, &ch.CorrelationRho); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	rows.Close()
	ids := map[string]struct{}{}
	// Fetch link memberships in one query to keep topology validation simple.
	linkRows, err := s.DB.QueryContext(ctx,
		`SELECT channel_id,position,link_id FROM channel_links ORDER BY channel_id,position`)
	if err != nil {
		return nil, err
	}
	defer linkRows.Close()
	linksByChannel := map[string][]string{}
	for linkRows.Next() {
		var chID, linkID string
		var pos int
		if err := linkRows.Scan(&chID, &pos, &linkID); err != nil {
			return nil, err
		}
		linksByChannel[chID] = append(linksByChannel[chID], linkID)
	}
	if err := linkRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].LinkIDs = linksByChannel[out[i].ID]
		ids[out[i].ID] = struct{}{}
	}
	return out, nil
}

// EndpointLinks returns link IDs containing an endpoint.
func (s *Store) EndpointLinks(ctx context.Context, endpointID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id FROM links WHERE end_a_id=? OR end_b_id=? ORDER BY id`, endpointID, endpointID)
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

// LinkChannels returns channel IDs containing a link.
func (s *Store) LinkChannels(ctx context.Context, linkID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT DISTINCT channel_id FROM channel_links WHERE link_id=? ORDER BY channel_id`, linkID)
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
