package params

import (
	"context"

	"microwave-network/internal/domain"
	"microwave-network/internal/store"
)

type Source struct {
	Field    string  `json:"field"`
	Value    float64 `json:"value"`
	Layer    string  `json:"layer"`
	ScopeID  string  `json:"scope_id"`
	Version  string  `json:"version"`
	RawField string  `json:"raw_field"`
	Derived  bool    `json:"derived,omitempty"`
}

type EffectiveEndpoint struct {
	Endpoint domain.Endpoint     `json:"endpoint"`
	Values   map[string]float64  `json:"values"`
	Sources  map[string]Source   `json:"sources"`
	Refs     []domain.VersionRef `json:"refs"`
}

type resolvedVersion struct {
	Layer   string
	ScopeID string
	Version string
	Fields  map[string]float64
}

// Effective merges an endpoint using the fixed precedence:
// radio nominal < batch correction < site installation overrides, and antenna
// nominal for antenna values. Missing fields inherit the next layer.
func (s *Service) Effective(ctx context.Context, e domain.Endpoint, refs []domain.VersionRef) (EffectiveEndpoint, error) {
	return EffectiveWithStore(ctx, s.store, e, refs)
}

func EffectiveWithStore(ctx context.Context, st store.Tx, e domain.Endpoint, refs []domain.VersionRef) (EffectiveEndpoint, error) {
	if err := checkReferences(ctx, st, e); err != nil {
		return EffectiveEndpoint{}, err
	}
	rv := map[string]resolvedVersion{}
	for _, ref := range refs {
		var v domain.Version
		if err := st.GetDoc(ctx, colVersions, ref.Version, &v); err != nil {
			if err == domain.ErrNotFound {
				return EffectiveEndpoint{}, domain.Invalid(domain.FieldError{Field: "refs." + ref.Version, Message: "version does not exist"})
			}
			return EffectiveEndpoint{}, err
		}
		if v.Layer != ref.Layer || v.ScopeID != ref.ScopeID {
			return EffectiveEndpoint{}, domain.Invalid(domain.FieldError{Field: "refs." + ref.Version, Message: "version layer or scope does not match the reference"})
		}
		rv[v.Layer+"\x00"+v.ScopeID] = resolvedVersion{Layer: v.Layer, ScopeID: v.ScopeID, Version: v.Version, Fields: v.Fields}
	}
	out := EffectiveEndpoint{Endpoint: e, Values: map[string]float64{}, Sources: map[string]Source{}, Refs: store.VersionRefs(refs)}
	radioCandidates := []resolvedVersion{
		rv[domain.LayerRadioModel+"\x00"+e.RadioModelID],
		rv[domain.LayerBatch+"\x00"+e.BatchID],
	}
	// Site can also override radio characteristics because unusual station
	// installations may include tuned radios; site always has highest precedence.
	radioCandidates = append(radioCandidates, rv[domain.LayerSite+"\x00"+e.SiteID])
	resolveRadio := func(field, effective string, def float64, hasDefault bool) {
		var val float64
		var found bool
		var src resolvedVersion
		for _, c := range radioCandidates {
			if c.Fields == nil {
				continue
			}
			if v, ok := c.Fields[field]; ok {
				val, found, src = v, true, c
			}
		}
		if !found && hasDefault {
			val, found = def, true
		}
		if found {
			out.Values[effective] = val
			if src.Version == "" {
				out.Sources[effective] = Source{Field: effective, Value: val, Layer: "default", ScopeID: "system", RawField: field}
			} else {
				out.Sources[effective] = Source{Field: effective, Value: val, Layer: src.Layer, ScopeID: src.ScopeID, Version: src.Version, RawField: field}
			}
		}
	}
	resolveRadio(domain.FieldTxPower, domain.EffectiveTxPower, 0, false)
	resolveRadio(domain.FieldRxThreshold, domain.EffectiveRxThreshold, 0, false)
	resolveRadio(domain.FieldFeederLossPerM, domain.EffectiveFeederLossPerM, 0, false)

	// Installation numeric values default to zero; their raw source is shown as
	// system default when not covered by any layer.
	resolveInstall := func(field, effective string, def float64) {
		val, found := def, false
		var src resolvedVersion
		for _, c := range radioCandidates {
			if c.Fields == nil {
				continue
			}
			if v, ok := c.Fields[field]; ok {
				val, found, src = v, true, c
			}
		}
		if !found {
			val = def
		}
		out.Values[effective] = val
		if found {
			out.Sources[effective] = Source{Field: effective, Value: val, Layer: src.Layer, ScopeID: src.ScopeID, Version: src.Version, RawField: field}
		} else {
			out.Sources[effective] = Source{Field: effective, Value: val, Layer: "default", ScopeID: "system", RawField: field}
		}
	}
	resolveInstall(domain.FieldFeederLength, domain.EffectiveFeederLength, 0)
	resolveInstall(domain.FieldSplitterLoss, domain.EffectiveSplitterLoss, 0)
	resolveInstall(domain.FieldSplitterCount, domain.EffectiveSplitterCount, 0)

	ant := rv[domain.LayerAntenna+"\x00"+e.AntennaModelID]
	if ant.Fields != nil {
		if v, ok := ant.Fields[domain.FieldAntennaGain]; ok {
			out.Values[domain.EffectiveAntennaGain] = v
			out.Sources[domain.EffectiveAntennaGain] = Source{Field: domain.EffectiveAntennaGain, Value: v, Layer: ant.Layer, ScopeID: ant.ScopeID, Version: ant.Version, RawField: domain.FieldAntennaGain}
		}
	}

	requiredFields := map[string]string{
		domain.EffectiveTxPower:        "radio.tx_power_dbm",
		domain.EffectiveRxThreshold:    "radio.rx_threshold_dbm",
		domain.EffectiveFeederLossPerM: "radio.feeder_loss_db_per_m",
	}
	for _, field := range []string{domain.EffectiveTxPower, domain.EffectiveRxThreshold, domain.EffectiveFeederLossPerM} {
		if _, ok := out.Values[field]; !ok {
			return EffectiveEndpoint{}, domain.Invalid(domain.FieldError{Field: requiredFields[field], Message: "has no value in any published layer"})
		}
	}
	if _, ok := out.Values[domain.EffectiveAntennaGain]; !ok {
		return EffectiveEndpoint{}, domain.Invalid(domain.FieldError{Field: "antenna.antenna_gain_dbi", Message: "has no published antenna model value"})
	}

	perM := out.Values[domain.EffectiveFeederLossPerM]
	length := out.Values[domain.EffectiveFeederLength]
	splitterLoss := out.Values[domain.EffectiveSplitterLoss]
	count := out.Values[domain.EffectiveSplitterCount]
	totalFeeder := perM * length
	totalSplitter := splitterLoss * count
	total := totalFeeder + totalSplitter
	out.Values[domain.EffectiveTotalFeederLoss] = totalFeeder
	out.Values[domain.EffectiveTotalSplitterLoss] = totalSplitter
	out.Values[domain.EffectiveTotalLoss] = total
	for k, v := range map[string]float64{
		domain.EffectiveTotalFeederLoss:   totalFeeder,
		domain.EffectiveTotalSplitterLoss: totalSplitter,
		domain.EffectiveTotalLoss:         total,
	} {
		out.Sources[k] = Source{Field: k, Value: v, Layer: "derived", ScopeID: "system", Derived: true,
			RawField: domain.FieldFeederLength + "," + domain.FieldFeederLossPerM + "," + domain.FieldSplitterLoss + "," + domain.FieldSplitterCount}
	}
	return out, nil
}

func checkReferences(ctx context.Context, st store.Tx, e domain.Endpoint) error {
	var errs []domain.FieldError
	var radio domain.RadioModel
	if err := st.GetDoc(ctx, colRadioModels, e.RadioModelID, &radio); err != nil {
		errs = append(errs, domain.FieldError{Field: "radio_model_id", Message: "referenced radio model does not exist"})
	} else if e.FrequencyMHz < radio.FMinMHz || e.FrequencyMHz > radio.FMaxMHz {
		errs = append(errs, domain.FieldError{Field: "frequency_mhz", Message: "frequency is outside this radio model's supported band"})
	}
	var ant domain.AntennaModel
	if err := st.GetDoc(ctx, colAntennas, e.AntennaModelID, &ant); err != nil {
		errs = append(errs, domain.FieldError{Field: "antenna_model_id", Message: "referenced antenna model does not exist"})
	}
	if err := st.GetDoc(ctx, colSites, e.SiteID, &domain.Site{}); err != nil {
		errs = append(errs, domain.FieldError{Field: "site_id", Message: "referenced site does not exist"})
	}
	if e.BatchID != "" {
		if err := st.GetDoc(ctx, colBatches, e.BatchID, &domain.Batch{}); err != nil {
			errs = append(errs, domain.FieldError{Field: "batch_id", Message: "referenced batch does not exist"})
		}
	}
	if len(errs) > 0 {
		return domain.Invalid(errs...)
	}
	return nil
}

// ValidateEndpoint exposes reference and frequency validation to link writes.
func ValidateEndpoint(ctx context.Context, st store.Tx, e domain.Endpoint) error {
	return checkReferences(ctx, st, e)
}
