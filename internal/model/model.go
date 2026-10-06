// Package model contains persisted entities and cross-service data structures.
package model

import (
	"time"

	"microwaveplanner/internal/channel"
	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/params"
)

// ParamSet is one immutable revision of one scope at one layer.
type ParamSet struct {
	ID          string             `json:"id"`
	Layer       params.Layer       `json:"layer"`
	ScopeKind   string             `json:"scopeKind"`
	ScopeID     string             `json:"scopeId"`
	Version     int64              `json:"version"`
	Fields      map[string]float64 `json:"fields"`
	PublishedAt time.Time          `json:"publishedAt"`
	PublishedBy string             `json:"publishedBy,omitempty"`
	Comment     string             `json:"comment,omitempty"`
}

// ParamDraft is mutable unpublished content.
type ParamDraft struct {
	Layer     params.Layer       `json:"layer"`
	ScopeKind string             `json:"scopeKind"`
	ScopeID   string             `json:"scopeId"`
	Fields    map[string]float64 `json:"fields"`
	Revision  int64              `json:"revision"`
	UpdatedAt time.Time          `json:"updatedAt"`
	UpdatedBy string             `json:"updatedBy,omitempty"`
}

// LayerSnapshot is the set of maximum versions captured by an evaluation.
type LayerSnapshot map[string]int64

// LayerVersions records all three logical layer head versions.
type LayerVersions struct {
	Nominal int64 `json:"nominal"`
	Batch   int64 `json:"batch"`
	Site    int64 `json:"site"`
}

// Site is a physical station.
type Site struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Region string `json:"rainClimateRegionId,omitempty"`
	Remark string `json:"remark,omitempty"`
}

// Endpoint is one installed radio/antenna end.
type Endpoint struct {
	ID             string `json:"id"`
	SiteID         string `json:"siteId"`
	Name           string `json:"name"`
	RadioModelID   string `json:"radioModelId"`
	RadioBatchID   string `json:"radioBatchId,omitempty"`
	AntennaModelID string `json:"antennaModelId"`
	AntennaBatchID string `json:"antennaBatchId,omitempty"`
}

// EndpointSnapshot freezes the six set versions used to resolve one end.
type EndpointSnapshot struct {
	EndpointID            string `json:"endpointId"`
	RadioNominalVersion   int64  `json:"radioNominalVersion"`
	RadioBatchVersion     int64  `json:"radioBatchVersion"`
	RadioSiteVersion      int64  `json:"radioSiteVersion"`
	AntennaNominalVersion int64  `json:"antennaNominalVersion"`
	AntennaBatchVersion   int64  `json:"antennaBatchVersion"`
	AntennaSiteVersion    int64  `json:"antennaSiteVersion"`
}

// Link is one microwave hop between two sites.
type Link struct {
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	EndAID        string                  `json:"endAId"`
	EndBID        string                  `json:"endBId"`
	SiteAID       string                  `json:"siteAId"`
	SiteBID       string                  `json:"siteBId"`
	FrequencyGHz  float64                 `json:"frequencyGHz"`
	DistanceKM    float64                 `json:"distanceKm"`
	Polarization  linkbudget.Polarization `json:"polarization"`
	RainRegionAID string                  `json:"rainRegionAId,omitempty"`
	RainRegionBID string                  `json:"rainRegionBId,omitempty"`
	// RainRegionID chooses the controlling rain climatic region. Regional
	// planning data normally assigns one region to a hop; if blank the service
	// uses the A site's region.
	RainRegionID       string  `json:"rainRegionId,omitempty"`
	RainBandID         string  `json:"rainBandId,omitempty"`
	TargetAvailability float64 `json:"targetAvailability,omitempty"`
}

// Channel is an ordered tandem business path.
type Channel struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	LinkIDs            []string `json:"linkIds"`
	TargetAvailability float64  `json:"targetAvailability"`
	CorrelationRho     float64  `json:"correlationRho"`
}

// RainClimate is rainfall statistics.
type RainClimate struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	R001 float64 `json:"r001MMh"`
}

// RainBandCoefficient is a P.838 coefficient row.
type RainBandCoefficient struct {
	ID           string                  `json:"id"`
	BandID       string                  `json:"bandId"`
	FrequencyGHz float64                 `json:"frequencyGHz"`
	Polarization linkbudget.Polarization `json:"polarization"`
	K            float64                 `json:"k"`
	Alpha        float64                 `json:"alpha"`
}

// EffectiveEndpointView extends the pure resolved endpoint with references.
type EffectiveEndpointView struct {
	*params.EffectiveEndpoint
	SiteID         string `json:"siteId,omitempty"`
	RadioModelID   string `json:"radioModelId,omitempty"`
	RadioBatchID   string `json:"radioBatchId,omitempty"`
	AntennaModelID string `json:"antennaModelId,omitempty"`
	AntennaBatchID string `json:"antennaBatchId,omitempty"`
}

// LinkEvaluation is a persisted hop assessment.
type LinkEvaluation struct {
	ID                 string            `json:"id"`
	LinkID             string            `json:"linkId"`
	Kind               string            `json:"kind"` // instant, recompute
	TaskID             string            `json:"taskId,omitempty"`
	EvaluatedAt        time.Time         `json:"evaluatedAt"`
	LayerVersions      map[string]any    `json:"layerVersions"`
	InputHash          string            `json:"inputHash"`
	Budget             linkbudget.Budget `json:"budget"`
	RainRegionID       string            `json:"rainRegionId"`
	RainR001           float64           `json:"rainR001"`
	RainK              float64           `json:"rainK"`
	RainAlpha          float64           `json:"rainAlpha"`
	RainP              float64           `json:"rainUnavailabilityPercent"`
	Availability       float64           `json:"availability"`
	ValidRange         bool              `json:"rainMethodInValidRange"`
	TargetAvailability float64           `json:"targetAvailability,omitempty"`
	MeetsTarget        bool              `json:"meetsTarget"`
	Snapshot           map[string]any    `json:"snapshot"`
}

// ChannelEvaluation is a persisted tandem assessment.
type ChannelEvaluation struct {
	ID                 string         `json:"id"`
	ChannelID          string         `json:"channelId"`
	Kind               string         `json:"kind"`
	TaskID             string         `json:"taskId,omitempty"`
	EvaluatedAt        time.Time      `json:"evaluatedAt"`
	LayerVersions      map[string]any `json:"layerVersions"`
	InputHash          string         `json:"inputHash"`
	HopP               []float64      `json:"hopUnavailabilityFraction"`
	Combined           channel.Result `json:"combined"`
	TargetAvailability float64        `json:"targetAvailability"`
	MeetsTarget        bool           `json:"meetsTarget"`
	LinkEvaluationIDs  []string       `json:"linkEvaluationIds"`
	Snapshot           map[string]any `json:"snapshot"`
}

// RecomputeTask is a background recomputation task.
type RecomputeTask struct {
	ID          string        `json:"id"`
	Status      string        `json:"status"`
	Reason      string        `json:"reason"`
	Trigger     string        `json:"trigger"` // manual, publish
	Baseline    LayerVersions `json:"baseline"`
	Target      LayerVersions `json:"target"`
	Progress    TaskProgress  `json:"progress"`
	SubmittedAt time.Time     `json:"submittedAt"`
	StartedAt   *time.Time    `json:"startedAt,omitempty"`
	FinishedAt  *time.Time    `json:"finishedAt,omitempty"`
	Error       string        `json:"error,omitempty"`
	Affected    TaskAffected  `json:"affected"`
	Diff        TaskDiff      `json:"diff"`
	FollowupOf  string        `json:"followupOf,omitempty"`
}

// TaskProgress is completion counts.
type TaskProgress struct {
	TotalEndpoints int `json:"totalEndpoints"`
	DoneEndpoints  int `json:"doneEndpoints"`
	TotalLinks     int `json:"totalLinks"`
	DoneLinks      int `json:"doneLinks"`
	TotalChannels  int `json:"totalChannels"`
	DoneChannels   int `json:"doneChannels"`
}

// TaskAffected records exact dependency impact.
type TaskAffected struct {
	Endpoints []string `json:"endpoints"`
	Links     []string `json:"links"`
	Channels  []string `json:"channels"`
}

// MarginChange is one hop's before/after margin.
type MarginChange struct {
	LinkID       string  `json:"linkId"`
	BeforeMargin float64 `json:"beforeMarginDb"`
	AfterMargin  float64 `json:"afterMarginDb"`
	DeltaDB      float64 `json:"deltaDb"`
}

// ChannelAvailabilityChange records target crossing.
type ChannelAvailabilityChange struct {
	ChannelID          string  `json:"channelId"`
	BeforeAvailability float64 `json:"beforeAvailability"`
	AfterAvailability  float64 `json:"afterAvailability"`
	Target             float64 `json:"target"`
	BeforeMeetsTarget  bool    `json:"beforeMeetsTarget"`
	AfterMeetsTarget   bool    `json:"afterMeetsTarget"`
}

// TaskDiff is the management comparison output.
type TaskDiff struct {
	MarginDecreased      []MarginChange              `json:"marginDecreased"`
	MarginIncreased      []MarginChange              `json:"marginIncreased"`
	ChannelsCrossedBelow []ChannelAvailabilityChange `json:"channelsCrossedBelowTarget"`
	ChannelsRecovered    []ChannelAvailabilityChange `json:"channelsRecovered"`
}
