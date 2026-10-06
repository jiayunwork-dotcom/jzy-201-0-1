package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/model"
)

// CreateRainBandCoefficient inserts P.838 coefficients.
func (s *Store) CreateRainBandCoefficient(ctx context.Context, x model.RainBandCoefficient) error {
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO rain_band_coefficients(id,band_id,frequency_ghz,polarization,k,alpha,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		x.ID, x.BandID, x.FrequencyGHz, string(x.Polarization), x.K, x.Alpha, now, now)
	return err
}

// RainBandCoefficient selects coefficients for a band/polarization.
func (s *Store) RainBandCoefficient(ctx context.Context, bandID string, pol linkbudget.Polarization) (*model.RainBandCoefficient, error) {
	var x model.RainBandCoefficient
	var p string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,band_id,frequency_ghz,polarization,k,alpha
		 FROM rain_band_coefficients WHERE band_id=? AND polarization=?`,
		bandID, string(pol)).Scan(&x.ID, &x.BandID, &x.FrequencyGHz, &p, &x.K, &x.Alpha)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: rain coefficient band=%s polarization=%s", ErrNotFound, bandID, pol)
	}
	if err != nil {
		return nil, err
	}
	x.Polarization = linkbudget.Polarization(p)
	return &x, nil
}
