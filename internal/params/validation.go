package params

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"microwave-network/internal/domain"
	"microwave-network/internal/store"
)

var radioFields = map[string]bool{
	domain.FieldTxPower:        true,
	domain.FieldRxThreshold:    true,
	domain.FieldFeederLossPerM: true,
	domain.FieldFeederLength:   true,
	domain.FieldSplitterLoss:   true,
	domain.FieldSplitterCount:  true,
}
var antennaFields = map[string]bool{domain.FieldAntennaGain: true}
var installationFields = map[string]bool{
	domain.FieldFeederLength:  true,
	domain.FieldSplitterLoss:  true,
	domain.FieldSplitterCount: true,
}

func validateLayerField(layer, scope string, fields map[string]float64) error {
	if scope == "" {
		return domain.Invalid(domain.FieldError{Field: "scope_id", Message: "is required"})
	}
	var allowed map[string]bool
	fieldPath := "fields"
	switch layer {
	case domain.LayerModel:
		allowed = map[string]bool{}
		for k := range radioFields {
			allowed[k] = true
		}
		allowed[domain.FieldAntennaGain] = true
	case domain.LayerBatch:
		allowed = radioFields
	case domain.LayerSite:
		allowed = installationFields
	default:
		return domain.Invalid(domain.FieldError{Field: "layer", Message: "must be model, batch or site"})
	}
	var errs []domain.FieldError
	for k, v := range fields {
		if !allowed[k] {
			errs = append(errs, domain.FieldError{Field: fieldPath + "." + k, Message: "field is not allowed on layer " + layer})
			continue
		}
		if !finite(v) {
			errs = append(errs, domain.FieldError{Field: fieldPath + "." + k, Message: "must be a finite number"})
			continue
		}
		if (k == domain.FieldFeederLength || k == domain.FieldFeederLossPerM || k == domain.FieldSplitterLoss ||
			k == domain.FieldSplitterCount || k == domain.FieldAntennaGain) && v < 0 {
			errs = append(errs, domain.FieldError{Field: fieldPath + "." + k, Message: "must not be negative"})
		}
	}
	if len(errs) > 0 {
		return domain.Invalid(errs...)
	}
	return nil
}

func validateScopeExists(ctx context.Context, st store.Tx, layer, scope string) error {
	var collection string
	switch layer {
	case domain.LayerModel:
		collection = colRadioModels
		if err := st.GetDoc(ctx, collection, scope, &domain.RadioModel{}); err == nil {
			return nil
		}
		collection = colAntennas
		if err := st.GetDoc(ctx, collection, scope, &domain.AntennaModel{}); err == nil {
			return nil
		}
		return domain.Invalid(domain.FieldError{Field: "scope_id", Message: "references neither an existing radio model nor antenna model"})
	case domain.LayerBatch:
		collection = colBatches
	case domain.LayerSite:
		collection = colSites
	default:
		return nil
	}
	if err := st.GetDoc(ctx, collection, scope, &map[string]any{}); err != nil {
		return domain.Invalid(domain.FieldError{Field: "scope_id", Message: "references non-existent " + layer})
	}
	return nil
}

func validateScopeFields(ctx context.Context, st store.Tx, layer, scope string, fields map[string]float64) error {
	if layer != domain.LayerModel {
		return nil
	}
	var errs []domain.FieldError
	var radio domain.RadioModel
	isRadio := st.GetDoc(ctx, colRadioModels, scope, &radio) == nil
	var antenna domain.AntennaModel
	isAntenna := st.GetDoc(ctx, colAntennas, scope, &antenna) == nil
	for k := range fields {
		if isRadio && !radioFields[k] {
			errs = append(errs, domain.FieldError{Field: "fields." + k, Message: "is not a radio model parameter"})
		}
		if isAntenna && !antennaFields[k] {
			errs = append(errs, domain.FieldError{Field: "fields." + k, Message: "is not an antenna model parameter"})
		}
	}
	if !isRadio && !isAntenna {
		return domain.Invalid(domain.FieldError{Field: "scope_id", Message: "references neither an existing radio model nor antenna model"})
	}
	if len(errs) > 0 {
		return domain.Invalid(errs...)
	}
	return nil
}

func versionName(layer, scope string, seq int64) string {
	// Versions are lexicographically sortable within a scope.
	return fmt.Sprintf("%s.%s.v%06d", layer, scope, seq)
}
func versionSequence(v string) int64 {
	parts := strings.Split(v, ".v")
	if len(parts) != 2 {
		return 0
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}
	return n
}
