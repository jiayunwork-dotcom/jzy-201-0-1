package budget

import (
	"fmt"

	"microwave-network/internal/domain"
	"microwave-network/internal/params"
)

type Direction struct {
	Name             string  `json:"name"`
	TransmitSiteID   string  `json:"transmit_site_id"`
	ReceiveSiteID    string  `json:"receive_site_id"`
	TxPowerDBm       float64 `json:"tx_power_dbm"`
	TxLossDB         float64 `json:"tx_loss_db"`
	TxAntennaGainDBi float64 `json:"tx_antenna_gain_dbi"`
	RxAntennaGainDBi float64 `json:"rx_antenna_gain_dbi"`
	RxLossDB         float64 `json:"rx_loss_db"`
	RxThresholdDBm   float64 `json:"rx_threshold_dbm"`
	FreeSpaceLossDB  float64 `json:"free_space_loss_db"`
	RxLevelDBm       float64 `json:"rx_level_dbm"`
	FadeMarginDB     float64 `json:"fade_margin_db"`
}

type Result struct {
	LinkID            string                   `json:"link_id"`
	FrequencyMHz      float64                  `json:"frequency_mhz"`
	DistanceKm        float64                  `json:"distance_km"`
	DirectionAToB     Direction                `json:"direction_a_to_b"`
	DirectionBToA     Direction                `json:"direction_b_to_a"`
	FreeSpaceLossDB   float64                  `json:"free_space_loss_db"`
	WorstFadeMarginDB float64                  `json:"worst_fade_margin_db"`
	Warnings          []string                 `json:"warnings,omitempty"`
	EffectiveA        params.EffectiveEndpoint `json:"effective_a"`
	EffectiveB        params.EffectiveEndpoint `json:"effective_b"`
}

// Evaluate is deliberately symmetric: it contains both directional budgets and
// selects the worse fade margin for rain availability.
func Evaluate(link domain.Link, a, b params.EffectiveEndpoint) (Result, error) {
	fspl := FreeSpaceLoss(link.A.FrequencyMHz, link.DistanceKm)
	dab := direction("A->B", link.A.SiteID, link.B.SiteID, a, b, fspl)
	dba := direction("B->A", link.B.SiteID, link.A.SiteID, b, a, fspl)
	r := Result{
		LinkID: link.ID, FrequencyMHz: link.A.FrequencyMHz, DistanceKm: link.DistanceKm,
		DirectionAToB: dab, DirectionBToA: dba, FreeSpaceLossDB: fspl,
		WorstFadeMarginDB: min(dab.FadeMarginDB, dba.FadeMarginDB),
		EffectiveA:        a, EffectiveB: b,
	}
	r.Warnings = append(r.Warnings, thresholdWarnings(dab, dba)...)
	return r, nil
}

func direction(name, txSite, rxSite string, tx, rx params.EffectiveEndpoint, fspl float64) Direction {
	txPower := tx.Values[domain.EffectiveTxPower]
	txLoss := tx.Values[domain.EffectiveTotalLoss]
	txGain := tx.Values[domain.EffectiveAntennaGain]
	rxGain := rx.Values[domain.EffectiveAntennaGain]
	rxLoss := rx.Values[domain.EffectiveTotalLoss]
	threshold := rx.Values[domain.EffectiveRxThreshold]
	level := txPower - txLoss + txGain - fspl + rxGain - rxLoss
	return Direction{
		Name: name, TransmitSiteID: txSite, ReceiveSiteID: rxSite,
		TxPowerDBm: txPower, TxLossDB: txLoss, TxAntennaGainDBi: txGain,
		RxAntennaGainDBi: rxGain, RxLossDB: rxLoss, RxThresholdDBm: threshold,
		FreeSpaceLossDB: fspl, RxLevelDBm: level, FadeMarginDB: level - threshold,
	}
}

// thresholdWarnings checks zero-distance physical maximum receive level. A
// negative fade margin over the actual path is still returned as a result; this
// warning identifies a parameter set which cannot work even before propagation.
func thresholdWarnings(dab, dba Direction) []string {
	var out []string
	check := func(d Direction) {
		maxRx := d.TxPowerDBm - d.TxLossDB + d.TxAntennaGainDBi + d.RxAntennaGainDBi - d.RxLossDB
		if d.RxThresholdDBm > maxRx {
			out = append(out, fmt.Sprintf("%s: receiver threshold %.3f dBm is above theoretical maximum receive level %.3f dBm", d.Name, d.RxThresholdDBm, maxRx))
		}
	}
	check(dab)
	check(dba)
	return out
}
