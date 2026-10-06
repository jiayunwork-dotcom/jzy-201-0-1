// Package linkbudget computes deterministic microwave hop budgets.
package linkbudget

import (
	"fmt"
	"math"
	"strings"

	"microwaveplanner/internal/num"
	"microwaveplanner/internal/params"
)

// Polarization selects the rain coefficient set.
type Polarization string

const (
	PolarizationHorizontal Polarization = "H"
	PolarizationVertical   Polarization = "V"
)

// FieldError reports one invalid request field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationErrors is a named error containing one or more field errors.
type ValidationErrors struct {
	Errors []FieldError `json:"errors"`
}

func (e *ValidationErrors) Error() string {
	var b strings.Builder
	for i, fe := range e.Errors {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(fe.Field + ": " + fe.Message)
	}
	return b.String()
}

func addError(errs *[]FieldError, field, message string) {
	*errs = append(*errs, FieldError{Field: field, Message: message})
}

// HopInput is a physical hop and its two resolved ends.
type HopInput struct {
	LinkID       string
	SiteAID      string
	SiteBID      string
	EndA         *params.EffectiveEndpoint
	EndB         *params.EffectiveEndpoint
	FrequencyGHz float64
	DistanceKM   float64
	Polarization Polarization
}

// EndBudget details the local gains and losses at one end.
type EndBudget struct {
	EndpointID       string  `json:"endpointId"`
	AntennaGainDB    float64 `json:"antennaGainDb"`
	FeederLengthM    float64 `json:"feederLengthM"`
	FeederLossDB     float64 `json:"feederLossDb"`
	SplitterLossDB   float64 `json:"splitterLossDb"`
	ExtraLossDB      float64 `json:"extraLossDb"`
	TotalLocalLossDB float64 `json:"totalLocalLossDb"`
}

// Budget is the result of evaluating both propagation directions of one hop
// under dry-air conditions. The reported values are the worse direction, which
// makes the result invariant when EndA and EndB are exchanged.
type Budget struct {
	LinkID          string          `json:"linkId"`
	DirectionAtoB   DirectionBudget `json:"directionAtoB"`
	DirectionBtoA   DirectionBudget `json:"directionBtoA"`
	EndA            EndBudget       `json:"endA"`
	EndB            EndBudget       `json:"endB"`
	FreeSpaceLossDB float64         `json:"freeSpaceLossDb"`
	// The following four scalar fields describe the limiting direction.
	TransmitPowerDBm    float64      `json:"transmitPowerDbm"`
	ReceiveThresholdDBm float64      `json:"receiveThresholdDbm"`
	ReceivedLevelDBm    float64      `json:"receivedLevelDbm"`
	FadeMarginDB        float64      `json:"fadeMarginDb"`
	TheoreticalMaxRxDBm float64      `json:"theoreticalMaxRxDbm"`
	Warnings            []FieldError `json:"warnings,omitempty"`
}

// DirectionBudget is one transmitter-to-receiver calculation.
type DirectionBudget struct {
	TransmitPowerDBm    float64 `json:"transmitPowerDbm"`
	ReceiveThresholdDBm float64 `json:"receiveThresholdDbm"`
	// TheoreticalMaxReceiveDBm is transmit power plus antenna gains less all
	// local losses, before free-space propagation loss. It is the largest
	// receive level this installation could ever achieve.
	TheoreticalMaxReceiveDBm float64 `json:"theoreticalMaxReceiveDbm"`
	ReceivedLevelDBm         float64 `json:"receivedLevelDbm"`
	FadeMarginDB             float64 `json:"fadeMarginDb"`
}

// FreeSpaceLossDB implements ITU-R P.520 / standard line-of-sight free-space
// attenuation:
//
//	Lfs = 92.45 + 20 log10(d_km) + 20 log10(f_GHz)
//
// The constant corresponds to distances in kilometres and frequencies in GHz.
func FreeSpaceLossDB(distanceKM, frequencyGHz float64) float64 {
	return 92.45 + 20.0*math.Log10(distanceKM) + 20.0*math.Log10(frequencyGHz)
}

// Evaluate validates and evaluates a hop. Validation errors are hard errors.
// An impossible receiver threshold is returned as a warning because it still
// produces a mathematically valid, negative-margin budget.
func Evaluate(in HopInput) (*Budget, error) {
	var errs []FieldError

	if math.IsNaN(in.FrequencyGHz) || math.IsInf(in.FrequencyGHz, 0) || in.FrequencyGHz <= 0 {
		addError(&errs, "frequencyGHz", "frequency must be a positive finite number")
	}
	if math.IsNaN(in.DistanceKM) || math.IsInf(in.DistanceKM, 0) || in.DistanceKM <= 0 {
		addError(&errs, "distanceKM", "distance must be positive")
	}

	a, errA := evaluateEnd("endA", in.EndA, in.FrequencyGHz)
	b, errB := evaluateEnd("endB", in.EndB, in.FrequencyGHz)
	errs = append(errs, errA...)
	errs = append(errs, errB...)
	if len(errs) > 0 {
		return nil, &ValidationErrors{Errors: errs}
	}

	fspl := FreeSpaceLossDB(in.DistanceKM, in.FrequencyGHz)
	ab := direction(a, in.EndA, b, in.EndB, fspl)
	ba := direction(b, in.EndB, a, in.EndA, fspl)
	limiting := ab
	if ba.FadeMarginDB < ab.FadeMarginDB {
		limiting = ba
	}

	budget := &Budget{
		LinkID:              in.LinkID,
		DirectionAtoB:       ab,
		DirectionBtoA:       ba,
		EndA:                a,
		EndB:                b,
		FreeSpaceLossDB:     fspl,
		TransmitPowerDBm:    limiting.TransmitPowerDBm,
		ReceiveThresholdDBm: limiting.ReceiveThresholdDBm,
		ReceivedLevelDBm:    limiting.ReceivedLevelDBm,
		FadeMarginDB:        limiting.FadeMarginDB,
		TheoreticalMaxRxDBm: limiting.TheoreticalMaxReceiveDBm,
	}
	for _, d := range []DirectionBudget{ab, ba} {
		if d.ReceiveThresholdDBm > d.TheoreticalMaxReceiveDBm {
			budget.Warnings = append(budget.Warnings, FieldError{
				Field: "radio.rxThresholdDbm",
				Message: fmt.Sprintf("receiver threshold %.3f dBm is above the theoretical maximum receive level %.3f dBm; margin cannot be positive",
					d.ReceiveThresholdDBm, d.TheoreticalMaxReceiveDBm),
			})
		}
	}
	return budget, nil
}

func direction(txEnd EndBudget, txEP *params.EffectiveEndpoint,
	rxEnd EndBudget, rxEP *params.EffectiveEndpoint, fspl float64) DirectionBudget {
	tx, _ := txEP.RadioValue(params.FieldRadioTxPowerDBm)
	threshold, _ := rxEP.RadioValue(params.FieldRadioRxThresholdDBm)
	maxRx := tx.Value + txEnd.AntennaGainDB + rxEnd.AntennaGainDB -
		txEnd.TotalLocalLossDB - rxEnd.TotalLocalLossDB
	rx := maxRx - fspl
	return DirectionBudget{
		TransmitPowerDBm:         tx.Value,
		ReceiveThresholdDBm:      threshold.Value,
		ReceivedLevelDBm:         rx,
		FadeMarginDB:             rx - threshold.Value,
		TheoreticalMaxReceiveDBm: maxRx,
	}
}

type endInput struct {
	Endpoint *params.EffectiveEndpoint
	EndBudget
}

func evaluateEnd(path string, ep *params.EffectiveEndpoint, frequency float64) (EndBudget, []FieldError) {
	var errs []FieldError
	if ep == nil {
		addError(&errs, path, "resolved endpoint is missing")
		return EndBudget{}, errs
	}
	out := EndBudget{EndpointID: ep.EndpointID}

	tx, ok := ep.RadioValue(params.FieldRadioTxPowerDBm)
	if !ok {
		addError(&errs, path+".radio.txPowerDbm", "missing nominal transmitter power")
	} else if requireFinite(path+".radio.txPowerDbm", tx.Value, &errs) {
	}
	threshold, ok := ep.RadioValue(params.FieldRadioRxThresholdDBm)
	if !ok {
		addError(&errs, path+".radio.rxThresholdDbm", "missing nominal receiver threshold")
	} else {
		requireFinite(path+".radio.rxThresholdDbm", threshold.Value, &errs)
	}
	minF, hasMin := ep.RadioValue(params.FieldRadioMinFrequencyGHz)
	maxF, hasMax := ep.RadioValue(params.FieldRadioMaxFrequencyGHz)
	if !hasMin || !hasMax {
		addError(&errs, path+".radio.frequencyBand", "radio model must declare supported frequency band")
	} else if !math.IsNaN(frequency) && !math.IsInf(frequency, 0) && frequency > 0 &&
		(frequency < minF.Value || frequency > maxF.Value) {
		addError(&errs, path+".radio.frequencyBand",
			fmt.Sprintf("frequency %.6g GHz is outside radio model supported band %.6g-%.6g GHz", frequency, minF.Value, maxF.Value))
	}

	gain, ok := ep.AntennaValue(params.FieldAntennaGainDBD)
	if !ok {
		addError(&errs, path+".antenna.gainDb", "missing antenna gain")
	} else if requireFinite(path+".antenna.gainDb", gain.Value, &errs) {
		out.AntennaGainDB = gain.Value
	}

	// Feeder loss per metre normally has a nominal default. A missing site
	// feeder length means zero installed feeder for budget purposes, but it is
	// usually worth carrying explicitly. We do not make it an error.
	lossPerM, _ := ep.RadioValue(params.FieldRadioFeederLossDBpm)
	requireFinite(path+".radio.feederLossDbPerM", lossPerM.Value, &errs)
	length, hasLength := ep.InstallationValue(params.FieldInstallationFeederM)
	if hasLength {
		requireFinite(path+".installation.feederLengthM", length.Value, &errs)
		out.FeederLengthM = length.Value
	}
	out.FeederLossDB = out.FeederLengthM * lossPerM.Value
	requireFinite(path+".feederLossDb", out.FeederLossDB, &errs)

	if splitter, ok := ep.RadioValue(params.FieldRadioSplitterLossDB); ok {
		requireFinite(path+".radio.splitterLossDb", splitter.Value, &errs)
		out.SplitterLossDB = splitter.Value
	}
	if extra, ok := ep.InstallationValue(params.FieldInstallationExtraDB); ok {
		requireFinite(path+".installation.extraLossDb", extra.Value, &errs)
		out.ExtraLossDB = extra.Value
	}
	out.TotalLocalLossDB = out.FeederLossDB + out.SplitterLossDB + out.ExtraLossDB
	requireFinite(path+".totalLocalLossDb", out.TotalLocalLossDB, &errs)
	return out, errs
}

func requireFinite(field string, value float64, errs *[]FieldError) bool {
	if !num.Finite(value) {
		addError(errs, field, "value must be a finite number")
		return false
	}
	return true
}
