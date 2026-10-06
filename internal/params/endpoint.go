package params

// Equipment/field names used by the microwave model. Keeping them as constants
// avoids silently diverging between storage, merging and link budget code.
const (
	FieldRadioTxPowerDBm      = "radio.txPowerDbm"
	FieldRadioRxThresholdDBm  = "radio.rxThresholdDbm"
	FieldRadioMinFrequencyGHz = "radio.minFrequencyGHz"
	FieldRadioMaxFrequencyGHz = "radio.maxFrequencyGHz"
	FieldRadioFeederLossDBpm  = "radio.feederLossDbPerM"
	FieldRadioSplitterLossDB  = "radio.splitterLossDb"
	FieldAntennaGainDBD       = "antenna.gainDb"
	FieldAntennaHeightM       = "antenna.heightM"
	FieldInstallationFeederM  = "installation.feederLengthM"
	FieldInstallationExtraDB  = "installation.extraLossDb"
)

// EndpointInput contains the resolved versioned parameter sets for one end of
// a hop. Batch and site are optional; nominal radio/antenna sets are required
// before a link can be evaluated.
type EndpointInput struct {
	EndpointID     string
	RadioNominal   *VersionedSet
	RadioBatch     *VersionedSet
	RadioSite      *VersionedSet
	AntennaNominal *VersionedSet
	AntennaBatch   *VersionedSet
	AntennaSite    *VersionedSet
}

// EffectiveEndpoint is the complete effective view of one installed end.
type EffectiveEndpoint struct {
	EndpointID string           `json:"endpointId"`
	Radio      *EffectiveParams `json:"radio"`
	Antenna    *EffectiveParams `json:"antenna"`
	// Layers records the version actually present at each logical layer.
	// A zero version means that layer had no set and contributed nothing.
	Layers []LayerRef `json:"layers"`
}

// LayerRef records the version of one logical set at one end.
type LayerRef struct {
	Kind    string `json:"kind"` // radio or antenna
	Layer   Layer  `json:"layer"`
	Version int64  `json:"version"`
	SetID   string `json:"setId,omitempty"`
}

// ResolveEndpoint merges six independent parameter sets into one end.
//
// Radio and antenna parameters are merged separately. At site layer, the
// radio site set may also carry installation fields; antenna site sets carry
// only antenna fields.
func ResolveEndpoint(in EndpointInput) *EffectiveEndpoint {
	radio := MergeFields(map[Layer]*VersionedSet{
		LayerNominal: in.RadioNominal,
		LayerBatch:   in.RadioBatch,
		LayerSite:    in.RadioSite,
	})
	antenna := MergeFields(map[Layer]*VersionedSet{
		LayerNominal: in.AntennaNominal,
		LayerBatch:   in.AntennaBatch,
		LayerSite:    in.AntennaSite,
	})

	return &EffectiveEndpoint{
		EndpointID: in.EndpointID,
		Radio:      radio,
		Antenna:    antenna,
		Layers:     collectLayers(in),
	}
}

func collectLayers(in EndpointInput) []LayerRef {
	var refs []LayerRef
	add := func(kind string, layer Layer, vs *VersionedSet) {
		ref := LayerRef{Kind: kind, Layer: layer}
		if vs != nil {
			ref.Version = vs.Ref.Version
			ref.SetID = vs.SetID
		}
		refs = append(refs, ref)
	}
	for _, layer := range LayerOrder {
		switch layer {
		case LayerNominal:
			add("radio", layer, in.RadioNominal)
			add("antenna", layer, in.AntennaNominal)
		case LayerBatch:
			add("radio", layer, in.RadioBatch)
			add("antenna", layer, in.AntennaBatch)
		case LayerSite:
			add("radio", layer, in.RadioSite)
			add("antenna", layer, in.AntennaSite)
		}
	}
	return refs
}

// RadioValue and AntennaValue provide typed access used by the budget.
func (e *EffectiveEndpoint) field(kind, name string) (Field, bool) {
	switch kind {
	case "radio":
		f, ok := e.Radio.Fields[name]
		return f, ok
	case "antenna":
		f, ok := e.Antenna.Fields[name]
		return f, ok
	}
	return Field{}, false
}

// InstallationValue reads installation values, which live on the radio site set.
func (e *EffectiveEndpoint) InstallationValue(name string) (Field, bool) {
	return e.field("radio", name)
}

// RadioValue returns a radio field.
func (e *EffectiveEndpoint) RadioValue(name string) (Field, bool) { return e.field("radio", name) }

// AntennaValue returns an antenna field.
func (e *EffectiveEndpoint) AntennaValue(name string) (Field, bool) { return e.field("antenna", name) }

// NumericHash includes every effective radio and antenna value. It does not
// include endpoint identity: this makes swapped A/B ends compare equal.
func (e *EffectiveEndpoint) NumericHash() string {
	parts := []string{e.Radio.Hash(), e.Antenna.Hash()}
	return stableSHA256(joinHash(parts))
}

func joinHash(parts []string) string {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
		b = append(b, '|')
	}
	return string(b)
}
