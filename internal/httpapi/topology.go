package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/model"
	"microwaveplanner/internal/num"
)

type siteRequest struct {
	ID     string `json:"id" binding:"required"`
	Name   string `json:"name" binding:"required"`
	Region string `json:"rainClimateRegionId"`
	Remark string `json:"remark"`
}

type endpointRequest struct {
	ID             string `json:"id" binding:"required"`
	SiteID         string `json:"siteId" binding:"required"`
	Name           string `json:"name" binding:"required"`
	RadioModelID   string `json:"radioModelId" binding:"required"`
	RadioBatchID   string `json:"radioBatchId"`
	AntennaModelID string `json:"antennaModelId" binding:"required"`
	AntennaBatchID string `json:"antennaBatchId"`
}

type linkRequest struct {
	ID                 string                  `json:"id" binding:"required"`
	Name               string                  `json:"name" binding:"required"`
	EndAID             string                  `json:"endAId" binding:"required"`
	EndBID             string                  `json:"endBId" binding:"required"`
	FrequencyGHz       float64                 `json:"frequencyGHz"`
	DistanceKM         float64                 `json:"distanceKm"`
	Polarization       linkbudget.Polarization `json:"polarization"`
	RainRegionID       string                  `json:"rainRegionId"`
	RainBandID         string                  `json:"rainBandId"`
	TargetAvailability float64                 `json:"targetAvailability"`
}

type channelRequest struct {
	ID                 string   `json:"id" binding:"required"`
	Name               string   `json:"name" binding:"required"`
	LinkIDs            []string `json:"linkIds" binding:"required,min=1"`
	TargetAvailability float64  `json:"targetAvailability"`
	CorrelationRho     float64  `json:"correlationRho"`
}

type climateRequest struct {
	ID   string  `json:"id" binding:"required"`
	Name string  `json:"name" binding:"required"`
	R001 float64 `json:"r001MMh"`
}

type coefficientRequest struct {
	ID           string                  `json:"id" binding:"required"`
	BandID       string                  `json:"bandId" binding:"required"`
	FrequencyGHz float64                 `json:"frequencyGHz"`
	Polarization linkbudget.Polarization `json:"polarization"`
	K            float64                 `json:"k"`
	Alpha        float64                 `json:"alpha"`
}

func (s *Server) createSite(c *gin.Context) {
	var req siteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	site := model.Site{ID: req.ID, Name: req.Name, Region: req.Region, Remark: req.Remark}
	if req.Region != "" {
		if _, err := s.Store.GetRainClimate(c.Request.Context(), req.Region); err != nil {
			badRequest(c, err)
			return
		}
	}
	if err := s.Store.CreateSite(c.Request.Context(), site); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(201, site)
}

func (s *Server) updateSite(c *gin.Context) {
	var req siteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	site := model.Site{ID: c.Param("id"), Name: req.Name, Region: req.Region, Remark: req.Remark}
	if site.ID != req.ID {
		badRequest(c, &linkbudget.ValidationErrors{Errors: []linkbudget.FieldError{{Field: "id", Message: "path and body id must match"}}})
		return
	}
	if req.Region != "" {
		if _, err := s.Store.GetRainClimate(c.Request.Context(), req.Region); err != nil {
			badRequest(c, err)
			return
		}
	}
	if err := s.Store.UpdateSite(c.Request.Context(), site); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, site)
}

func (s *Server) listSites(c *gin.Context) {
	v, err := s.Store.ListSites(c.Request.Context())
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) getSite(c *gin.Context) {
	v, err := s.Store.GetSite(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) createRainClimate(c *gin.Context) {
	var req climateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	errs := validateFinite("r001MMh", req.R001, nil)
	if req.R001 <= 0 {
		errs = append(errs, linkbudget.FieldError{Field: "r001MMh", Message: "rain rate must be positive"})
	}
	if len(errs) > 0 {
		badRequest(c, &linkbudget.ValidationErrors{Errors: errs})
		return
	}
	x := model.RainClimate{ID: req.ID, Name: req.Name, R001: req.R001}
	if err := s.Store.CreateRainClimate(c.Request.Context(), x); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(201, x)
}

func (s *Server) getRainClimate(c *gin.Context) {
	v, err := s.Store.GetRainClimate(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) createRainCoefficient(c *gin.Context) {
	var req coefficientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	var errs []linkbudget.FieldError
	if req.FrequencyGHz <= 0 || !num.Finite(req.FrequencyGHz) {
		errs = append(errs, linkbudget.FieldError{Field: "frequencyGHz", Message: "must be positive finite"})
	}
	if !num.Finite(req.K) || req.K < 0 {
		errs = append(errs, linkbudget.FieldError{Field: "k", Message: "must be a non-negative finite number"})
	}
	if !num.Finite(req.Alpha) || req.Alpha < 0 {
		errs = append(errs, linkbudget.FieldError{Field: "alpha", Message: "must be a non-negative finite number"})
	}
	if req.Polarization != linkbudget.PolarizationHorizontal && req.Polarization != linkbudget.PolarizationVertical {
		errs = append(errs, linkbudget.FieldError{Field: "polarization", Message: "must be H or V"})
	}
	if len(errs) > 0 {
		badRequest(c, &linkbudget.ValidationErrors{Errors: errs})
		return
	}
	x := model.RainBandCoefficient{ID: req.ID, BandID: req.BandID, FrequencyGHz: req.FrequencyGHz,
		Polarization: req.Polarization, K: req.K, Alpha: req.Alpha}
	if err := s.Store.CreateRainBandCoefficient(c.Request.Context(), x); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(201, x)
}

func (s *Server) createEndpoint(c *gin.Context) {
	var req endpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	ep := model.Endpoint{ID: req.ID, SiteID: req.SiteID, Name: req.Name,
		RadioModelID: req.RadioModelID, RadioBatchID: req.RadioBatchID,
		AntennaModelID: req.AntennaModelID, AntennaBatchID: req.AntennaBatchID}
	if err := s.Store.CreateEndpoint(c.Request.Context(), ep); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(201, ep)
}

func (s *Server) updateEndpoint(c *gin.Context) {
	var req endpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	ep := model.Endpoint{ID: c.Param("id"), SiteID: req.SiteID, Name: req.Name,
		RadioModelID: req.RadioModelID, RadioBatchID: req.RadioBatchID,
		AntennaModelID: req.AntennaModelID, AntennaBatchID: req.AntennaBatchID}
	if ep.ID != req.ID {
		badRequest(c, &linkbudget.ValidationErrors{Errors: []linkbudget.FieldError{{Field: "id", Message: "path and body id must match"}}})
		return
	}
	if err := s.Store.UpdateEndpoint(c.Request.Context(), ep); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, ep)
}

func (s *Server) listEndpoints(c *gin.Context) {
	v, err := s.Store.ListEndpoints(c.Request.Context())
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) getEndpoint(c *gin.Context) {
	v, err := s.Store.GetEndpoint(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) createLink(c *gin.Context) {
	var req linkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	l, err := s.buildValidatedLink(c, req)
	if err != nil {
		badRequest(c, err)
		return
	}
	if err := s.Store.CreateLink(c.Request.Context(), l); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(http.StatusCreated, l)
}

func (s *Server) buildValidatedLink(c *gin.Context, req linkRequest) (model.Link, error) {
	var errs []linkbudget.FieldError
	if req.DistanceKM <= 0 || !num.Finite(req.DistanceKM) {
		errs = append(errs, linkbudget.FieldError{Field: "distanceKm", Message: "distance must be a positive finite number"})
	}
	if req.FrequencyGHz <= 0 || !num.Finite(req.FrequencyGHz) {
		errs = append(errs, linkbudget.FieldError{Field: "frequencyGHz", Message: "frequency must be a positive finite number"})
	}
	if req.Polarization != linkbudget.PolarizationHorizontal && req.Polarization != linkbudget.PolarizationVertical {
		errs = append(errs, linkbudget.FieldError{Field: "polarization", Message: "must be H or V"})
	}
	if req.TargetAvailability < 0 || req.TargetAvailability > 1 || (req.TargetAvailability != 0 && !num.Finite(req.TargetAvailability)) {
		errs = append(errs, linkbudget.FieldError{Field: "targetAvailability", Message: "must be between 0 and 1 when supplied"})
	}
	if len(errs) > 0 {
		return model.Link{}, &linkbudget.ValidationErrors{Errors: errs}
	}
	a, err := s.Store.GetEndpoint(c.Request.Context(), req.EndAID)
	if err != nil {
		return model.Link{}, err
	}
	b, err := s.Store.GetEndpoint(c.Request.Context(), req.EndBID)
	if err != nil {
		return model.Link{}, err
	}
	if a.SiteID == b.SiteID {
		return model.Link{}, &linkbudget.ValidationErrors{Errors: []linkbudget.FieldError{{
			Field: "endBId", Message: "the two ends must be installed at different sites",
		}}}
	}
	l := model.Link{ID: req.ID, Name: req.Name, EndAID: a.ID, EndBID: b.ID,
		SiteAID: a.SiteID, SiteBID: b.SiteID, FrequencyGHz: req.FrequencyGHz,
		DistanceKM: req.DistanceKM, Polarization: req.Polarization,
		RainRegionID: req.RainRegionID, RainBandID: req.RainBandID,
		TargetAvailability: req.TargetAvailability}
	av, _, errA := s.Store.ResolveEndpoint(c.Request.Context(), a.ID)
	bv, _, errB := s.Store.ResolveEndpoint(c.Request.Context(), b.ID)
	if errA != nil {
		return model.Link{}, errA
	}
	if errB != nil {
		return model.Link{}, errB
	}
	if _, err := linkbudget.Evaluate(linkbudget.HopInput{
		LinkID: l.ID, EndA: av.EffectiveEndpoint, EndB: bv.EffectiveEndpoint,
		FrequencyGHz: l.FrequencyGHz, DistanceKM: l.DistanceKM, Polarization: l.Polarization,
	}); err != nil {
		return model.Link{}, err
	}
	if l.RainRegionID != "" {
		if _, err := s.Store.GetRainClimate(c.Request.Context(), l.RainRegionID); err != nil {
			return model.Link{}, err
		}
	}
	return l, nil
}

func (s *Server) updateLink(c *gin.Context) {
	var req linkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	req.ID = c.Param("id")
	l, err := s.buildValidatedLink(c, req)
	if err != nil {
		badRequest(c, err)
		return
	}
	if err := s.Store.UpdateLink(c.Request.Context(), l); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, l)
}

func (s *Server) listLinks(c *gin.Context) {
	v, err := s.Store.ListLinks(c.Request.Context())
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) getLink(c *gin.Context) {
	v, err := s.Store.GetLink(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) createChannel(c *gin.Context) {
	var req channelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	ch, err := s.buildChannel(c, req.ID, req)
	if err != nil {
		badRequest(c, err)
		return
	}
	if err := s.Store.CreateChannel(c.Request.Context(), ch); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(201, ch)
}

func (s *Server) buildChannel(c *gin.Context, id string, req channelRequest) (model.Channel, error) {
	if req.CorrelationRho == 0 {
		req.CorrelationRho = 0.6
	}
	if req.CorrelationRho < 0 || req.CorrelationRho > 1 || !num.Finite(req.CorrelationRho) {
		return model.Channel{}, &linkbudget.ValidationErrors{Errors: []linkbudget.FieldError{{Field: "correlationRho", Message: "must be between 0 and 1"}}}
	}
	if req.TargetAvailability < 0 || req.TargetAvailability > 1 {
		return model.Channel{}, &linkbudget.ValidationErrors{Errors: []linkbudget.FieldError{{Field: "targetAvailability", Message: "must be between 0 and 1"}}}
	}
	ch := model.Channel{ID: id, Name: req.Name, LinkIDs: req.LinkIDs,
		TargetAvailability: req.TargetAvailability, CorrelationRho: req.CorrelationRho}
	if err := s.Assessment.ValidateTopology(c.Request.Context(), &ch); err != nil {
		return model.Channel{}, err
	}
	return ch, nil
}

func (s *Server) updateChannel(c *gin.Context) {
	var req channelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	ch, err := s.buildChannel(c, c.Param("id"), req)
	if err != nil {
		badRequest(c, err)
		return
	}
	if err := s.Store.UpdateChannel(c.Request.Context(), ch); err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, ch)
}

func (s *Server) listChannels(c *gin.Context) {
	v, err := s.Store.ListChannels(c.Request.Context())
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) getChannel(c *gin.Context) {
	v, err := s.Store.GetChannel(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, v)
}

func validateFinite(field string, v float64, errs []linkbudget.FieldError) []linkbudget.FieldError {
	if !num.Finite(v) {
		return append(errs, linkbudget.FieldError{Field: field, Message: "must be a finite number"})
	}
	return errs
}
