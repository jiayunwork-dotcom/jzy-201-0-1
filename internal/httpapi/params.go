package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/params"
	"microwaveplanner/internal/store"
)

type draftRequest struct {
	Fields       map[string]float64 `json:"fields"`
	BaseRevision int64              `json:"baseRevision"`
	UpdatedBy    string             `json:"updatedBy"`
}

type publishRequest struct {
	PublishedBy string `json:"publishedBy"`
	Comment     string `json:"comment"`
}

func validLayer(v string) (params.Layer, bool) {
	l := params.Layer(v)
	for _, x := range params.LayerOrder {
		if x == l {
			return l, true
		}
	}
	return "", false
}

func validScope(layer params.Layer, kind string) bool {
	switch layer {
	case params.LayerNominal:
		return kind == "radio_model" || kind == "antenna_model"
	case params.LayerBatch:
		return kind == "radio_batch" || kind == "antenna_batch"
	case params.LayerSite:
		return kind == "endpoint_radio" || kind == "endpoint_antenna"
	}
	return false
}

func (s *Server) getDraft(c *gin.Context) {
	layer, ok := validLayer(c.Param("layer"))
	if !ok || !validScope(layer, c.Param("scopeKind")) {
		badRequest(c, invalidLayerScope())
		return
	}
	d, err := s.Store.GetDraft(c.Request.Context(), layer, c.Param("scopeKind"), c.Param("scopeId"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, d)
}

func (s *Server) putDraft(c *gin.Context) {
	layer, ok := validLayer(c.Param("layer"))
	if !ok || !validScope(layer, c.Param("scopeKind")) {
		badRequest(c, invalidLayerScope())
		return
	}
	var req draftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_json", "message": err.Error()})
		return
	}
	if req.Fields == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_failed", "fields": []gin.H{{
			"field": "fields", "message": "fields object is required",
		}}})
		return
	}
	d, err := s.Store.PutDraft(c.Request.Context(), store.PutDraftRequest{
		Layer: layer, ScopeKind: c.Param("scopeKind"), ScopeID: c.Param("scopeId"),
		Fields: req.Fields, BaseRevision: req.BaseRevision, UpdatedBy: req.UpdatedBy,
	})
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (s *Server) deleteDraft(c *gin.Context) {
	layer, ok := validLayer(c.Param("layer"))
	if !ok || !validScope(layer, c.Param("scopeKind")) {
		badRequest(c, invalidLayerScope())
		return
	}
	var req struct {
		Revision int64 `json:"revision"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := s.Store.DeleteDraft(c.Request.Context(), layer, c.Param("scopeKind"), c.Param("scopeId"), req.Revision); err != nil {
		badRequest(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) publishDraft(c *gin.Context) {
	layer, ok := validLayer(c.Param("layer"))
	if !ok || !validScope(layer, c.Param("scopeKind")) {
		badRequest(c, invalidLayerScope())
		return
	}
	var req publishRequest
	_ = c.ShouldBindJSON(&req)
	pub, task, err := s.Tasks.PublishAndAffect(c.Request.Context(), layer, c.Param("scopeKind"),
		c.Param("scopeId"), req.PublishedBy, req.Comment)
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"published": pub, "recomputeTaskId": task.ID, "task": task})
}

func (s *Server) layerHeads(c *gin.Context) {
	heads, err := s.Store.LayerHeads(c.Request.Context())
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, heads)
}

func (s *Server) getSet(c *gin.Context) {
	layer, ok := validLayer(c.Param("layer"))
	if !ok || !validScope(layer, c.Param("scopeKind")) {
		badRequest(c, invalidLayerScope())
		return
	}
	var version int64
	if _, err := parsePathInt64(c, "version", &version); err != nil {
		badRequest(c, err)
		return
	}
	ps, err := s.Store.SetAtVersion(c.Request.Context(), layer, c.Param("scopeKind"), c.Param("scopeId"), version)
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, ps)
}

func (s *Server) effectiveEndpoint(c *gin.Context) {
	v, ep, err := s.Store.ResolveEndpoint(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, gin.H{"endpoint": ep, "effective": v})
}

func invalidLayerScope() error {
	return &linkbudget.ValidationErrors{Errors: []linkbudget.FieldError{{
		Field:   "layer/scopeKind",
		Message: "valid combinations are nominal:radio_model|antenna_model, batch:radio_batch|antenna_batch, site:endpoint_radio|endpoint_antenna",
	}}}
}
