package service

import (
	"context"
	"math"
	"strings"

	"microwave-network/internal/domain"
	"microwave-network/internal/params"
	"microwave-network/internal/store"
)

const (
	colLinks    = "links"
	colChannels = "channels"
)

type Topology struct{ store store.Store }

func NewTopology(s store.Store) *Topology { return &Topology{store: s} }

func (t *Topology) SaveLink(ctx context.Context, l domain.Link) (domain.Link, error) {
	if strings.TrimSpace(l.ID) == "" {
		return l, domain.Invalid(domain.FieldError{Field: "id", Message: "is required"})
	}
	if strings.TrimSpace(l.Name) == "" {
		return l, domain.Invalid(domain.FieldError{Field: "name", Message: "is required"})
	}
	var errs []domain.FieldError
	if !math.IsNaN(l.DistanceKm) && !math.IsInf(l.DistanceKm, 0) && l.DistanceKm <= 0 {
		errs = append(errs, domain.FieldError{Field: "distance_km", Message: "must be positive"})
	} else if math.IsNaN(l.DistanceKm) || math.IsInf(l.DistanceKm, 0) {
		errs = append(errs, domain.FieldError{Field: "distance_km", Message: "must be a finite positive number"})
	}
	if strings.TrimSpace(l.ClimateZoneID) == "" {
		errs = append(errs, domain.FieldError{Field: "climate_zone_id", Message: "is required"})
	}
	if strings.TrimSpace(l.RainBandID) == "" {
		errs = append(errs, domain.FieldError{Field: "rain_band_id", Message: "is required"})
	}
	if errs != nil {
		return l, domain.Invalid(errs...)
	}
	if err := t.store.RunTx(ctx, func(tx store.Tx) error {
		if err := params.ValidateEndpoint(ctx, tx, l.A); err != nil {
			return prefixField(err, "a")
		}
		if err := params.ValidateEndpoint(ctx, tx, l.B); err != nil {
			return prefixField(err, "b")
		}
		if l.A.FrequencyMHz != l.B.FrequencyMHz {
			return domain.Invalid(domain.FieldError{Field: "b.frequency_mhz", Message: "both ends of a reciprocal link must use the same frequency"})
		}
		if err := tx.GetDoc(ctx, "climate_zones", l.ClimateZoneID, &domain.ClimateZone{}); err != nil {
			return domain.Invalid(domain.FieldError{Field: "climate_zone_id", Message: "does not exist"})
		}
		if err := tx.GetDoc(ctx, "rain_bands", l.RainBandID, &domain.RainBand{}); err != nil {
			return domain.Invalid(domain.FieldError{Field: "rain_band_id", Message: "does not exist"})
		}
		var band domain.RainBand
		_ = tx.GetDoc(ctx, "rain_bands", l.RainBandID, &band)
		if l.A.FrequencyMHz < band.FMinMHz || l.A.FrequencyMHz > band.FMaxMHz {
			return domain.Invalid(domain.FieldError{Field: "frequency_mhz", Message: "is outside the selected rain attenuation band"})
		}
		l.UpdatedAt = t.store.Now()
		return tx.PutDoc(ctx, colLinks, l.ID, l)
	}); err != nil {
		return l, err
	}
	return l, nil
}

func (t *Topology) GetLink(ctx context.Context, id string) (domain.Link, error) {
	var l domain.Link
	return l, t.store.GetDoc(ctx, colLinks, id, &l)
}
func (t *Topology) ListLinks(ctx context.Context) ([]domain.Link, error) {
	var out []domain.Link
	return out, t.store.ListDocs(ctx, colLinks, &out)
}
func (t *Topology) DeleteLink(ctx context.Context, id string) error {
	return t.store.DeleteDoc(ctx, colLinks, id)
}

func (t *Topology) SaveChannel(ctx context.Context, c domain.Channel) (domain.Channel, error) {
	var errs []domain.FieldError
	if strings.TrimSpace(c.ID) == "" {
		errs = append(errs, domain.FieldError{Field: "id", Message: "is required"})
	}
	if strings.TrimSpace(c.Name) == "" {
		errs = append(errs, domain.FieldError{Field: "name", Message: "is required"})
	}
	if len(c.LinkIDs) < 1 {
		errs = append(errs, domain.FieldError{Field: "link_ids", Message: "must contain at least one link"})
	}
	if c.TargetAvailability <= 0 || c.TargetAvailability >= 1 {
		errs = append(errs, domain.FieldError{Field: "target_availability", Message: "must be between 0 and 1"})
	}
	if errs != nil {
		return c, domain.Invalid(errs...)
	}
	err := t.store.RunTx(ctx, func(tx store.Tx) error {
		links := map[string]domain.Link{}
		for i, id := range c.LinkIDs {
			var l domain.Link
			if err := tx.GetDoc(ctx, colLinks, id, &l); err != nil {
				return domain.Invalid(domain.FieldError{Field: "link_ids", Message: "references non-existent link " + id})
			}
			links[id] = l
			if i > 0 {
				prev := links[c.LinkIDs[i-1]]
				if !sharesSite(prev, l) {
					return domain.Invalid(domain.FieldError{Field: "link_ids[" + itoa(i-1) + "," + itoa(i) + "]", Message: "adjacent links do not share a site"})
				}
			}
		}
		c.UpdatedAt = t.store.Now()
		return tx.PutDoc(ctx, colChannels, c.ID, c)
	})
	return c, err
}
func (t *Topology) GetChannel(ctx context.Context, id string) (domain.Channel, error) {
	var c domain.Channel
	return c, t.store.GetDoc(ctx, colChannels, id, &c)
}
func (t *Topology) ListChannels(ctx context.Context) ([]domain.Channel, error) {
	var out []domain.Channel
	return out, t.store.ListDocs(ctx, colChannels, &out)
}
func (t *Topology) DeleteChannel(ctx context.Context, id string) error {
	return t.store.DeleteDoc(ctx, colChannels, id)
}

func sharesSite(a, b domain.Link) bool {
	sites := map[string]bool{a.A.SiteID: true, a.B.SiteID: true, b.A.SiteID: true, b.B.SiteID: true}
	return len(sites) < 4
}
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
func prefixField(err error, prefix string) error {
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		return err
	}
	cp := &domain.ValidationError{}
	for _, e := range ve.Errs {
		e.Field = prefix + "." + e.Field
		cp.Errs = append(cp.Errs, e)
	}
	return cp
}
