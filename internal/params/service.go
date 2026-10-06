package params

import (
	"context"
	"math"
	"sort"
	"strings"

	"microwave-network/internal/domain"
	"microwave-network/internal/store"
)

const (
	colRadioModels = "radio_models"
	colAntennas    = "antenna_models"
	colSites       = "sites"
	colBatches     = "batches"
	colZones       = "climate_zones"
	colRainBands   = "rain_bands"
	colDrafts      = "drafts"
	colVersions    = "versions"
)

type Service struct {
	store store.Store
}

func New(s store.Store) *Service { return &Service{store: s} }

type SaveCatalogRequest struct {
	ID        string
	Name      string
	Vendor    string
	FMinMHz   float64
	FMaxMHz   float64
	IsRadio   bool
	IsAntenna bool
}

func (s *Service) SaveRadio(ctx context.Context, m domain.RadioModel) (domain.RadioModel, error) {
	var errs []domain.FieldError
	if strings.TrimSpace(m.ID) == "" {
		errs = append(errs, domain.FieldError{Field: "id", Message: "is required"})
	}
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, domain.FieldError{Field: "name", Message: "is required"})
	}
	if !finite(m.FMinMHz) || m.FMinMHz <= 0 {
		errs = append(errs, domain.FieldError{Field: "f_min_mhz", Message: "must be positive"})
	}
	if !finite(m.FMaxMHz) || m.FMaxMHz <= m.FMinMHz {
		errs = append(errs, domain.FieldError{Field: "f_max_mhz", Message: "must be greater than f_min_mhz"})
	}
	if errs != nil {
		return m, domain.Invalid(errs...)
	}
	if err := s.store.GetDoc(ctx, colAntennas, m.ID, &domain.AntennaModel{}); err != domain.ErrNotFound {
		if err == nil {
			return m, domain.Invalid(domain.FieldError{Field: "id", Message: "is already used by an antenna model"})
		}
		return m, err
	}
	m.Name = strings.TrimSpace(m.Name)
	m.Vendor = strings.TrimSpace(m.Vendor)
	if m.CreatedAt.IsZero() {
		m.CreatedAt = s.store.Now()
	}
	return m, s.store.PutDoc(ctx, colRadioModels, m.ID, m)
}
func (s *Service) GetRadio(ctx context.Context, id string) (domain.RadioModel, error) {
	var m domain.RadioModel
	return m, s.store.GetDoc(ctx, colRadioModels, id, &m)
}
func (s *Service) ListRadios(ctx context.Context) ([]domain.RadioModel, error) {
	var out []domain.RadioModel
	return out, s.store.ListDocs(ctx, colRadioModels, &out)
}

func (s *Service) SaveAntenna(ctx context.Context, a domain.AntennaModel) (domain.AntennaModel, error) {
	if strings.TrimSpace(a.ID) == "" {
		return a, domain.Invalid(domain.FieldError{Field: "id", Message: "is required"})
	}
	if strings.TrimSpace(a.Name) == "" {
		return a, domain.Invalid(domain.FieldError{Field: "name", Message: "is required"})
	}
	if err := s.store.GetDoc(ctx, colRadioModels, a.ID, &domain.RadioModel{}); err != domain.ErrNotFound {
		if err == nil {
			return a, domain.Invalid(domain.FieldError{Field: "id", Message: "is already used by a radio model"})
		}
		return a, err
	}
	a.Name, a.Vendor = strings.TrimSpace(a.Name), strings.TrimSpace(a.Vendor)
	if a.CreatedAt.IsZero() {
		a.CreatedAt = s.store.Now()
	}
	return a, s.store.PutDoc(ctx, colAntennas, a.ID, a)
}
func (s *Service) GetAntenna(ctx context.Context, id string) (domain.AntennaModel, error) {
	var a domain.AntennaModel
	return a, s.store.GetDoc(ctx, colAntennas, id, &a)
}
func (s *Service) ListAntennas(ctx context.Context) ([]domain.AntennaModel, error) {
	var out []domain.AntennaModel
	return out, s.store.ListDocs(ctx, colAntennas, &out)
}

func (s *Service) SaveSite(ctx context.Context, x domain.Site) (domain.Site, error) {
	if strings.TrimSpace(x.ID) == "" {
		return x, domain.Invalid(domain.FieldError{Field: "id", Message: "is required"})
	}
	x.Name = strings.TrimSpace(x.Name)
	if x.CreatedAt.IsZero() {
		x.CreatedAt = s.store.Now()
	}
	return x, s.store.PutDoc(ctx, colSites, x.ID, x)
}
func (s *Service) GetSite(ctx context.Context, id string) (domain.Site, error) {
	var x domain.Site
	return x, s.store.GetDoc(ctx, colSites, id, &x)
}
func (s *Service) ListSites(ctx context.Context) ([]domain.Site, error) {
	var out []domain.Site
	return out, s.store.ListDocs(ctx, colSites, &out)
}

func (s *Service) SaveBatch(ctx context.Context, x domain.Batch) (domain.Batch, error) {
	if strings.TrimSpace(x.ID) == "" {
		return x, domain.Invalid(domain.FieldError{Field: "id", Message: "is required"})
	}
	x.Name = strings.TrimSpace(x.Name)
	if x.CreatedAt.IsZero() {
		x.CreatedAt = s.store.Now()
	}
	return x, s.store.PutDoc(ctx, colBatches, x.ID, x)
}
func (s *Service) GetBatch(ctx context.Context, id string) (domain.Batch, error) {
	var x domain.Batch
	return x, s.store.GetDoc(ctx, colBatches, id, &x)
}
func (s *Service) ListBatches(ctx context.Context) ([]domain.Batch, error) {
	var out []domain.Batch
	return out, s.store.ListDocs(ctx, colBatches, &out)
}

func (s *Service) SaveClimate(ctx context.Context, x domain.ClimateZone) (domain.ClimateZone, error) {
	var errs []domain.FieldError
	if strings.TrimSpace(x.ID) == "" {
		errs = append(errs, domain.FieldError{Field: "id", Message: "is required"})
	}
	if !finite(x.RainRate01PctMMH) || x.RainRate01PctMMH < 0 {
		errs = append(errs, domain.FieldError{Field: "rain_rate_01_pct_mm_h", Message: "must be finite and non-negative"})
	}
	if errs != nil {
		return x, domain.Invalid(errs...)
	}
	x.Name = strings.TrimSpace(x.Name)
	if x.CreatedAt.IsZero() {
		x.CreatedAt = s.store.Now()
	}
	return x, s.store.PutDoc(ctx, colZones, x.ID, x)
}
func (s *Service) GetClimate(ctx context.Context, id string) (domain.ClimateZone, error) {
	var x domain.ClimateZone
	return x, s.store.GetDoc(ctx, colZones, id, &x)
}
func (s *Service) ListClimate(ctx context.Context) ([]domain.ClimateZone, error) {
	var out []domain.ClimateZone
	return out, s.store.ListDocs(ctx, colZones, &out)
}

func (s *Service) SaveRainBand(ctx context.Context, x domain.RainBand) (domain.RainBand, error) {
	var errs []domain.FieldError
	if strings.TrimSpace(x.ID) == "" {
		errs = append(errs, domain.FieldError{Field: "id", Message: "is required"})
	}
	if !finite(x.FMinMHz) || x.FMinMHz <= 0 {
		errs = append(errs, domain.FieldError{Field: "f_min_mhz", Message: "must be positive"})
	}
	if !finite(x.FMaxMHz) || x.FMaxMHz <= x.FMinMHz {
		errs = append(errs, domain.FieldError{Field: "f_max_mhz", Message: "must be greater than f_min_mhz"})
	}
	if !finite(x.K) || x.K <= 0 {
		errs = append(errs, domain.FieldError{Field: "k", Message: "must be positive"})
	}
	if !finite(x.Alpha) || x.Alpha <= 0 {
		errs = append(errs, domain.FieldError{Field: "alpha", Message: "must be positive"})
	}
	if errs != nil {
		return x, domain.Invalid(errs...)
	}
	x.Name = strings.TrimSpace(x.Name)
	if x.CreatedAt.IsZero() {
		x.CreatedAt = s.store.Now()
	}
	return x, s.store.PutDoc(ctx, colRainBands, x.ID, x)
}
func (s *Service) GetRainBand(ctx context.Context, id string) (domain.RainBand, error) {
	var x domain.RainBand
	return x, s.store.GetDoc(ctx, colRainBands, id, &x)
}
func (s *Service) ListRainBands(ctx context.Context) ([]domain.RainBand, error) {
	var out []domain.RainBand
	return out, s.store.ListDocs(ctx, colRainBands, &out)
}

type DraftPatch struct {
	Layer   string             `json:"layer"`
	ScopeID string             `json:"scope_id"`
	Upserts map[string]float64 `json:"upserts"`
	Deletes []string           `json:"deletes"`
	// ExpectedRevision enables optimistic concurrency. Omit only to create a draft.
	ExpectedRevision *int64 `json:"expected_revision,omitempty"`
}

// PatchDraft applies an upsert/delete patch. Every successful save increments
// one revision for that layer+scope, so a stale second writer receives 409.
func (s *Service) PatchDraft(ctx context.Context, p DraftPatch) (domain.Draft, error) {
	var out domain.Draft
	err := s.store.RunTx(ctx, func(tx store.Tx) error {
		if err := validateLayerField(p.Layer, p.ScopeID, p.Upserts); err != nil {
			return err
		}
		if len(p.Upserts) > 0 {
			if err := validateScopeFields(ctx, tx, p.Layer, p.ScopeID, p.Upserts); err != nil {
				return err
			}
		}
		if err := validateLayerField(p.Layer, p.ScopeID, sliceMap(p.Deletes)); err != nil {
			return err
		}
		if err := validateScopeExists(ctx, tx, p.Layer, p.ScopeID); err != nil {
			return err
		}
		id := draftID(p.Layer, p.ScopeID)
		var d domain.Draft
		if err := tx.GetDoc(ctx, colDrafts, id, &d); err != nil {
			if err != domain.ErrNotFound {
				return err
			}
			if p.ExpectedRevision != nil && *p.ExpectedRevision != 0 {
				return domain.ErrConflict
			}
			d = domain.Draft{Layer: p.Layer, ScopeID: p.ScopeID, Fields: map[string]float64{}}
		} else if p.ExpectedRevision != nil && *p.ExpectedRevision != d.Revision {
			return domain.ErrConflict
		}
		if d.Fields == nil {
			d.Fields = map[string]float64{}
		}
		for k, v := range p.Upserts {
			d.Fields[k] = v
		}
		for _, k := range p.Deletes {
			delete(d.Fields, k)
		}
		d.Revision++
		d.UpdatedAt = s.store.Now()
		out = d
		return tx.PutDoc(ctx, colDrafts, id, d)
	})
	return out, err
}

func (s *Service) GetDraft(ctx context.Context, layer, scope string) (domain.Draft, error) {
	var d domain.Draft
	return d, s.store.GetDoc(ctx, colDrafts, draftID(layer, scope), &d)
}

type PublishRequest struct {
	Layer   string `json:"layer"`
	ScopeID string `json:"scope_id"`
	Message string `json:"message"`
}

type PublishResult struct {
	Before  []domain.VersionRef `json:"before"`
	After   []domain.VersionRef `json:"after"`
	Version domain.Version      `json:"version"`
}

// Publish turns the current draft into an immutable version. Published versions
// are never altered; later publication simply adds another version document.
func (s *Service) Publish(ctx context.Context, req PublishRequest) (PublishResult, error) {
	var res PublishResult
	err := s.store.RunTx(ctx, func(tx store.Tx) error {
		var err error
		res, err = s.PublishTx(ctx, tx, req)
		return err
	})
	return res, err
}

// PublishTx allows the same publication row and downstream job row to be written
// in one database transaction.
func (s *Service) PublishTx(ctx context.Context, tx store.Tx, req PublishRequest) (PublishResult, error) {
	id := draftID(req.Layer, req.ScopeID)
	var d domain.Draft
	if err := tx.GetDoc(ctx, colDrafts, id, &d); err != nil {
		return PublishResult{}, err
	}
	if len(d.Fields) == 0 {
		return PublishResult{}, domain.Invalid(domain.FieldError{Field: "fields", Message: "cannot publish an empty layer; deleting all fields causes inheritance"})
	}
	if err := validateLayerField(req.Layer, req.ScopeID, d.Fields); err != nil {
		return PublishResult{}, err
	}
	if err := validateScopeExists(ctx, tx, req.Layer, req.ScopeID); err != nil {
		return PublishResult{}, err
	}
	if err := validateScopeFields(ctx, tx, req.Layer, req.ScopeID, d.Fields); err != nil {
		return PublishResult{}, err
	}
	before, err := latestRefsTx(ctx, tx)
	if err != nil {
		return PublishResult{}, err
	}
	prev, _ := latestVersionTx(ctx, tx, req.Layer, req.ScopeID)
	var seq int64 = 1
	if prev.Version != "" {
		seq = versionSequence(prev.Version) + 1
	}
	v := domain.Version{
		Layer:       req.Layer,
		ScopeID:     req.ScopeID,
		Version:     versionName(req.Layer, req.ScopeID, seq),
		Revision:    d.Revision,
		Fields:      cloneFields(d.Fields),
		Previous:    prev.Version,
		PublishedAt: s.store.Now(),
	}
	if err := tx.PutDoc(ctx, colVersions, v.Version, v); err != nil {
		return PublishResult{}, err
	}
	// The draft remains open for the next edit, at a new revision.
	d.Revision++
	d.UpdatedAt = s.store.Now()
	if err := tx.PutDoc(ctx, colDrafts, id, d); err != nil {
		return PublishResult{}, err
	}
	after, err := latestRefsTx(ctx, tx)
	if err != nil {
		return PublishResult{}, err
	}
	return PublishResult{Before: before, After: after, Version: v}, nil
}

func (s *Service) GetVersion(ctx context.Context, id string) (domain.Version, error) {
	var v domain.Version
	return v, s.store.GetDoc(ctx, colVersions, id, &v)
}
func (s *Service) ListVersions(ctx context.Context, layer, scope string) ([]domain.Version, error) {
	var all []domain.Version
	if err := s.store.ListDocs(ctx, colVersions, &all); err != nil {
		return nil, err
	}
	var out []domain.Version
	for _, v := range all {
		if v.Layer == layer && v.ScopeID == scope {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// LatestRefs returns a complete version snapshot. Impact jobs pin this snapshot.
func (s *Service) LatestRefs(ctx context.Context) ([]domain.VersionRef, error) {
	return latestRefs(ctx, s.store)
}

func latestRefs(ctx context.Context, tx store.Tx) ([]domain.VersionRef, error) {
	return latestRefsTx(ctx, tx)
}
func latestRefsTx(ctx context.Context, tx store.Tx) ([]domain.VersionRef, error) {
	var all []domain.Version
	if err := tx.ListDocs(ctx, colVersions, &all); err != nil {
		return nil, err
	}
	latest := map[string]domain.Version{}
	for _, v := range all {
		key := v.Layer + "\x00" + v.ScopeID
		if cur, ok := latest[key]; !ok || v.Version > cur.Version {
			latest[key] = v
		}
	}
	refs := make([]domain.VersionRef, 0, len(latest))
	for _, v := range latest {
		refs = append(refs, domain.VersionRef{Layer: v.Layer, ScopeID: v.ScopeID, Version: v.Version})
	}
	return store.VersionRefs(refs), nil
}

func latestVersionTx(ctx context.Context, tx store.Tx, layer, scope string) (domain.Version, error) {
	vs, err := versionsTx(ctx, tx, layer, scope)
	if err != nil || len(vs) == 0 {
		return domain.Version{}, err
	}
	return vs[len(vs)-1], nil
}
func versionsTx(ctx context.Context, tx store.Tx, layer, scope string) ([]domain.Version, error) {
	var all []domain.Version
	if err := tx.ListDocs(ctx, colVersions, &all); err != nil {
		return nil, err
	}
	var out []domain.Version
	for _, v := range all {
		if v.Layer == layer && v.ScopeID == scope {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func cloneFields(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func sliceMap(keys []string) map[string]float64 {
	out := map[string]float64{}
	for _, k := range keys {
		out[k] = 0
	}
	return out
}
func draftID(layer, scope string) string { return layer + "/" + scope }
