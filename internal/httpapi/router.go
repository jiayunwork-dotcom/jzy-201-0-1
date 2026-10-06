// Package httpapi exposes the planning service over Gin HTTP endpoints.
package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"microwaveplanner/internal/assessment"
	"microwaveplanner/internal/linkbudget"
	"microwaveplanner/internal/store"
	"microwaveplanner/internal/task"
	"microwaveplanner/internal/version"
)

// Server contains service dependencies.
type Server struct {
	Store      *store.Store
	Assessment *assessment.Service
	Tasks      *task.Manager
}

// NewRouter builds the only external interface: HTTP/JSON.
func NewRouter(s *Server) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestLog())

	api := r.Group("/api/v1")

	api.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// Layer drafts and publishing.
	api.GET("/layers/:layer/scopes/:scopeKind/:scopeId/draft", s.getDraft)
	api.PUT("/layers/:layer/scopes/:scopeKind/:scopeId/draft", s.putDraft)
	api.DELETE("/layers/:layer/scopes/:scopeKind/:scopeId/draft", s.deleteDraft)
	api.POST("/layers/:layer/scopes/:scopeKind/:scopeId/publish", s.publishDraft)
	api.GET("/layers/heads", s.layerHeads)
	api.GET("/layers/:layer/scopes/:scopeKind/:scopeId/versions/:version", s.getSet)

	// Effective parameter provenance.
	api.GET("/endpoints/:id/effective", s.effectiveEndpoint)

	// Topology.
	api.POST("/sites", s.createSite)
	api.PUT("/sites/:id", s.updateSite)
	api.GET("/sites", s.listSites)
	api.GET("/sites/:id", s.getSite)
	api.POST("/rain-climates", s.createRainClimate)
	api.GET("/rain-climates/:id", s.getRainClimate)
	api.POST("/rain-coefficients", s.createRainCoefficient)
	api.POST("/endpoints", s.createEndpoint)
	api.PUT("/endpoints/:id", s.updateEndpoint)
	api.GET("/endpoints", s.listEndpoints)
	api.GET("/endpoints/:id", s.getEndpoint)
	api.POST("/links", s.createLink)
	api.PUT("/links/:id", s.updateLink)
	api.GET("/links", s.listLinks)
	api.GET("/links/:id", s.getLink)
	api.POST("/channels", s.createChannel)
	api.PUT("/channels/:id", s.updateChannel)
	api.GET("/channels", s.listChannels)
	api.GET("/channels/:id", s.getChannel)

	// Evaluation and recomputation.
	api.POST("/links/:id/evaluate", s.evaluateLink)
	api.POST("/channels/:id/evaluate", s.evaluateChannel)
	api.POST("/recompute-tasks", s.submitManualTask)
	api.GET("/recompute-tasks/:id", s.getTask)
	api.GET("/recompute-tasks", s.listTasks)
	api.GET("/links/:id/evaluations", s.listLinkEvaluations)
	api.GET("/link-evaluations/:id", s.getLinkEvaluation)
	api.POST("/link-evaluations/:id/replay", s.replayLinkEvaluation)
	api.GET("/channels/:id/evaluations", s.listChannelEvaluations)
	api.GET("/channel-evaluations/:id", s.getChannelEvaluation)

	return r
}

func requestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func badRequest(c *gin.Context, err error) {
	var ve *linkbudget.ValidationErrors
	if errors.As(err, &ve) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_failed", "fields": ve.Errors})
		return
	}
	var conflict *version.Conflict
	if errors.As(err, &conflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "draft_conflict", "details": conflict})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": err.Error()})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "message": err.Error()})
}

func internal(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": err.Error()})
}
