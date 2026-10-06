package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) evaluateLink(c *gin.Context) {
	le, err := s.Assessment.EvaluateLink(c.Request.Context(), c.Param("id"), "instant", "", true)
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(http.StatusOK, le)
}

func (s *Server) evaluateChannel(c *gin.Context) {
	ce, _, err := s.Assessment.EvaluateChannel(c.Request.Context(), c.Param("id"), "instant", "", true)
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(http.StatusOK, ce)
}

func (s *Server) submitManualTask(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Reason == "" {
		req.Reason = "manual recomputation"
	}
	t, err := s.Tasks.SubmitManual(c.Request.Context(), req.Reason)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"taskId": t.ID, "task": t})
}

func (s *Server) getTask(c *gin.Context) {
	t, err := s.Store.GetTask(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, t)
}

func (s *Server) listTasks(c *gin.Context) {
	tasks, err := s.Store.ListTasks(c.Request.Context(), 100)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, tasks)
}

func (s *Server) listLinkEvaluations(c *gin.Context) {
	v, err := s.Store.ListLinkEvaluations(c.Request.Context(), c.Param("id"), 100)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) getLinkEvaluation(c *gin.Context) {
	v, err := s.Store.GetLinkEvaluation(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) replayLinkEvaluation(c *gin.Context) {
	result, err := s.Assessment.ReplayLinkEvaluation(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, result)
}

func (s *Server) listChannelEvaluations(c *gin.Context) {
	v, err := s.Store.ListChannelEvaluations(c.Request.Context(), c.Param("id"), 100)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) getChannelEvaluation(c *gin.Context) {
	v, err := s.Store.GetChannelEvaluation(c.Request.Context(), c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(200, v)
}
