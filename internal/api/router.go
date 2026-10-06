package api

import (
	"errors"
	"net/http"

	"microwave-network/internal/domain"
	"microwave-network/internal/service"
	"microwave-network/internal/task"

	"github.com/gin-gonic/gin"
)

type Handler struct{ App *service.App }

func New(app *service.App) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	h := Handler{App: app}
	api := r.Group("/api/v1")

	api.GET("/radios", h.listRadios)
	api.POST("/radios", h.saveRadio)
	api.GET("/radios/:id", h.getRadio)
	api.GET("/antennas", h.listAntennas)
	api.POST("/antennas", h.saveAntenna)
	api.GET("/antennas/:id", h.getAntenna)
	api.GET("/sites", h.listSites)
	api.POST("/sites", h.saveSite)
	api.GET("/sites/:id", h.getSite)
	api.GET("/batches", h.listBatches)
	api.POST("/batches", h.saveBatch)
	api.GET("/batches/:id", h.getBatch)
	api.GET("/climate-zones", h.listClimate)
	api.POST("/climate-zones", h.saveClimate)
	api.GET("/rain-bands", h.listRainBands)
	api.POST("/rain-bands", h.saveRainBand)

	api.GET("/drafts/:layer/:scope", h.getDraft)
	api.PUT("/drafts", h.patchDraft)
	api.POST("/publish", h.publish)
	api.GET("/versions/:id", h.getVersion)
	api.GET("/versions/:layer/:scope", h.listVersions)
	api.POST("/effective", h.effective)

	api.GET("/links", h.listLinks)
	api.PUT("/links/:id", h.saveLink)
	api.GET("/links/:id", h.getLink)
	api.DELETE("/links/:id", h.deleteLink)
	api.POST("/links/:id/evaluate", h.evaluateLink)

	api.GET("/channels", h.listChannels)
	api.PUT("/channels/:id", h.saveChannel)
	api.GET("/channels/:id", h.getChannel)
	api.DELETE("/channels/:id", h.deleteChannel)
	api.POST("/channels/:id/evaluate", h.evaluateChannel)

	api.GET("/jobs", h.listJobs)
	api.GET("/jobs/:id", h.getJob)
	api.POST("/jobs/:id/run", h.runJob)
	api.GET("/assessments", h.listAssessments)
	api.GET("/assessments/compare", h.compareAssessments)
	api.GET("/assessments/:id", h.getAssessment)
	api.POST("/assessments/:id/replay", h.replayAssessment)

	return r
}

func writeError(c *gin.Context, err error) {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input", "fields": ve.Errs})
		return
	}
	if errors.Is(err, domain.ErrConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "draft revision conflict"})
		return
	}
	if errors.Is(err, domain.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

func bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}

func jobStatus(j task.Job) gin.H {
	return gin.H{"id": j.ID, "status": j.Status, "progress": j.Progress, "total": j.Total, "result": j.Result, "error": j.ErrorMessage}
}
