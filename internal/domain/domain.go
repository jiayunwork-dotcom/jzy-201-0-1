package domain

import (
	"errors"
	"time"
)

// Layer names are stable and used in API payloads and persisted references.
const (
	// LayerModel is the nominal/manufacturer layer. Its scope is either a radio
	// model ID or an antenna model ID; allowed field names disambiguate them.
	LayerModel      = "model"
	LayerRadioModel = LayerModel
	LayerBatch      = "batch"
	LayerSite       = "site"
	LayerAntenna    = LayerModel
)

// Parameter names. Installations use plain names; radio and antenna names are
// namespaced when one effective document is returned for an endpoint.
const (
	FieldTxPower        = "tx_power_dbm"
	FieldRxThreshold    = "rx_threshold_dbm"
	FieldFeederLossPerM = "feeder_loss_db_per_m"
	FieldFeederLength   = "feeder_length_m"
	FieldSplitterLoss   = "splitter_loss_db"
	FieldSplitterCount  = "splitter_count"
	FieldAntennaGain    = "antenna_gain_dbi"
)

// Effective names keep the component boundary visible in API responses.
const (
	EffectiveTxPower           = "radio.tx_power_dbm"
	EffectiveRxThreshold       = "radio.rx_threshold_dbm"
	EffectiveFeederLossPerM    = "radio.feeder_loss_db_per_m"
	EffectiveFeederLength      = "installation.feeder_length_m"
	EffectiveSplitterLoss      = "installation.splitter_loss_db"
	EffectiveSplitterCount     = "installation.splitter_count"
	EffectiveAntennaGain       = "antenna.antenna_gain_dbi"
	EffectiveTotalFeederLoss   = "endpoint.total_feeder_loss_db"
	EffectiveTotalSplitterLoss = "endpoint.total_splitter_loss_db"
	EffectiveTotalLoss         = "endpoint.total_loss_db"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")
var ErrInvalid = errors.New("invalid input")

// FieldError reports the exact request field that failed validation.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct {
	Errs []FieldError `json:"errors"`
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Errs) == 0 {
		return ErrInvalid.Error()
	}
	return e.Errs[0].Field + ": " + e.Errs[0].Message
}
func (e *ValidationError) Unwrap() error { return ErrInvalid }

func Invalid(errs ...FieldError) error { return &ValidationError{Errs: errs} }

type Draft struct {
	Layer     string             `json:"layer"`
	ScopeID   string             `json:"scope_id"`
	Revision  int64              `json:"revision"`
	Fields    map[string]float64 `json:"fields"`
	UpdatedAt time.Time          `json:"updated_at"`
}

type Version struct {
	Layer       string             `json:"layer"`
	ScopeID     string             `json:"scope_id"`
	Version     string             `json:"version"`
	Revision    int64              `json:"revision"`
	Fields      map[string]float64 `json:"fields"`
	Previous    string             `json:"previous_version,omitempty"`
	PublishedAt time.Time          `json:"published_at"`
}

type VersionRef struct {
	Layer   string `json:"layer"`
	ScopeID string `json:"scope_id"`
	Version string `json:"version"`
}

type RadioModel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Vendor    string    `json:"vendor"`
	FMinMHz   float64   `json:"f_min_mhz"`
	FMaxMHz   float64   `json:"f_max_mhz"`
	CreatedAt time.Time `json:"created_at"`
}

type AntennaModel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Vendor    string    `json:"vendor"`
	CreatedAt time.Time `json:"created_at"`
}

type Site struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Batch struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Endpoint struct {
	SiteID         string  `json:"site_id"`
	RadioModelID   string  `json:"radio_model_id"`
	AntennaModelID string  `json:"antenna_model_id"`
	BatchID        string  `json:"batch_id"`
	FrequencyMHz   float64 `json:"frequency_mhz"`
}

type Link struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	A             Endpoint  `json:"a"`
	B             Endpoint  `json:"b"`
	DistanceKm    float64   `json:"distance_km"`
	ClimateZoneID string    `json:"climate_zone_id"`
	RainBandID    string    `json:"rain_band_id"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Channel struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	LinkIDs            []string  `json:"link_ids"`
	TargetAvailability float64   `json:"target_availability"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ClimateZone struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	RainRate01PctMMH float64   `json:"rain_rate_01_pct_mm_h"`
	CreatedAt        time.Time `json:"created_at"`
}

// RainBand carries ITU-R P.838 k and alpha for one polarization/frequency band.
type RainBand struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	FMinMHz   float64   `json:"f_min_mhz"`
	FMaxMHz   float64   `json:"f_max_mhz"`
	K         float64   `json:"k"`
	Alpha     float64   `json:"alpha"`
	CreatedAt time.Time `json:"created_at"`
}
