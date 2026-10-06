package api

import (
	"github.com/gin-gonic/gin"

	"microwave-network/internal/domain"
)

func (h Handler) saveLink(c *gin.Context) {
	var l domain.Link
	if !bind(c, &l) {
		return
	}
	l.ID = c.Param("id")
	out, err := h.App.Topology.SaveLink(c.Request.Context(), l)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) listLinks(c *gin.Context) {
	out, err := h.App.Topology.ListLinks(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) getLink(c *gin.Context) {
	out, err := h.App.Topology.GetLink(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) deleteLink(c *gin.Context) {
	if err := h.App.Topology.DeleteLink(c.Request.Context(), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(204)
}

func (h Handler) saveChannel(c *gin.Context) {
	var x domain.Channel
	if !bind(c, &x) {
		return
	}
	x.ID = c.Param("id")
	out, err := h.App.Topology.SaveChannel(c.Request.Context(), x)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) listChannels(c *gin.Context) {
	out, err := h.App.Topology.ListChannels(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) getChannel(c *gin.Context) {
	out, err := h.App.Topology.GetChannel(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) deleteChannel(c *gin.Context) {
	if err := h.App.Topology.DeleteChannel(c.Request.Context(), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(204)
}

type evalReq struct {
	Refs []domain.VersionRef `json:"refs"`
}

func refsOrLatest(c *gin.Context, h Handler, refs []domain.VersionRef) ([]domain.VersionRef, bool) {
	if refs != nil {
		return refs, true
	}
	out, err := h.App.Params.LatestRefs(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return nil, false
	}
	return out, true
}

func (h Handler) evaluateLink(c *gin.Context) {
	var r evalReq
	if c.Request.ContentLength > 0 {
		if !bind(c, &r) {
			return
		}
	}
	refs, ok := refsOrLatest(c, h, r.Refs)
	if !ok {
		return
	}
	out, err := h.App.Evaluator.EvaluateAndSaveLink(c.Request.Context(), c.Param("id"), refs)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) evaluateChannel(c *gin.Context) {
	var r evalReq
	if c.Request.ContentLength > 0 {
		if !bind(c, &r) {
			return
		}
	}
	refs, ok := refsOrLatest(c, h, r.Refs)
	if !ok {
		return
	}
	out, err := h.App.Evaluator.EvaluateAndSaveChannel(c.Request.Context(), c.Param("id"), refs)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}

func (h Handler) listJobs(c *gin.Context) {
	out, err := h.App.Tasks.List(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) getJob(c *gin.Context) {
	out, err := h.App.Tasks.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) runJob(c *gin.Context) {
	ran, err := h.App.Tasks.RunAvailable(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, gin.H{"ran": ran})
}
func (h Handler) listAssessments(c *gin.Context) {
	out, err := h.App.Evaluator.ListAssessments(c.Request.Context(), c.Query("subject_id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) getAssessment(c *gin.Context) {
	out, err := h.App.Evaluator.GetAssessment(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) compareAssessments(c *gin.Context) {
	out, err := h.App.Evaluator.CompareAssessments(c.Request.Context(), c.Query("left"), c.Query("right"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) replayAssessment(c *gin.Context) {
	out, err := h.App.Evaluator.Replay(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
