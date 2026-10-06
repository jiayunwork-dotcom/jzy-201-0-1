package api

import (
	"github.com/gin-gonic/gin"

	"microwave-network/internal/domain"
	"microwave-network/internal/params"
)

func (h Handler) patchDraft(c *gin.Context) {
	var p params.DraftPatch
	if !bind(c, &p) {
		return
	}
	out, err := h.App.Params.PatchDraft(c.Request.Context(), p)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) getDraft(c *gin.Context) {
	out, err := h.App.Params.GetDraft(c.Request.Context(), c.Param("layer"), c.Param("scope"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) publish(c *gin.Context) {
	var p params.PublishRequest
	if !bind(c, &p) {
		return
	}
	pr, job, err := h.App.PublishAndEnqueue(c.Request.Context(), p)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(202, gin.H{"publish": pr, "job": job})
}
func (h Handler) getVersion(c *gin.Context) {
	out, err := h.App.Params.GetVersion(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) listVersions(c *gin.Context) {
	out, err := h.App.Params.ListVersions(c.Request.Context(), c.Param("layer"), c.Param("scope"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}

type effectiveRequest struct {
	Endpoint domain.Endpoint     `json:"endpoint"`
	Refs     []domain.VersionRef `json:"refs"`
}

func (h Handler) effective(c *gin.Context) {
	var req effectiveRequest
	if !bind(c, &req) {
		return
	}
	refs := req.Refs
	if refs == nil {
		var err error
		refs, err = h.App.Params.LatestRefs(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
	}
	out, err := h.App.Params.Effective(c.Request.Context(), req.Endpoint, refs)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
