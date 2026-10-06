package api

import (
	"github.com/gin-gonic/gin"

	"microwave-network/internal/domain"
)

func (h Handler) saveRadio(c *gin.Context) {
	var x domain.RadioModel
	if !bind(c, &x) {
		return
	}
	out, err := h.App.Params.SaveRadio(c.Request.Context(), x)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) listRadios(c *gin.Context) {
	out, err := h.App.Params.ListRadios(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) getRadio(c *gin.Context) {
	out, err := h.App.Params.GetRadio(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}

func (h Handler) saveAntenna(c *gin.Context) {
	var x domain.AntennaModel
	if !bind(c, &x) {
		return
	}
	out, err := h.App.Params.SaveAntenna(c.Request.Context(), x)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) listAntennas(c *gin.Context) {
	out, err := h.App.Params.ListAntennas(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) getAntenna(c *gin.Context) {
	out, err := h.App.Params.GetAntenna(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}

func (h Handler) saveSite(c *gin.Context) {
	var x domain.Site
	if !bind(c, &x) {
		return
	}
	out, err := h.App.Params.SaveSite(c.Request.Context(), x)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) listSites(c *gin.Context) {
	out, err := h.App.Params.ListSites(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) getSite(c *gin.Context) {
	out, err := h.App.Params.GetSite(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}

func (h Handler) saveBatch(c *gin.Context) {
	var x domain.Batch
	if !bind(c, &x) {
		return
	}
	out, err := h.App.Params.SaveBatch(c.Request.Context(), x)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) listBatches(c *gin.Context) {
	out, err := h.App.Params.ListBatches(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) getBatch(c *gin.Context) {
	out, err := h.App.Params.GetBatch(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}

func (h Handler) saveClimate(c *gin.Context) {
	var x domain.ClimateZone
	if !bind(c, &x) {
		return
	}
	out, err := h.App.Params.SaveClimate(c.Request.Context(), x)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) listClimate(c *gin.Context) {
	out, err := h.App.Params.ListClimate(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}

func (h Handler) saveRainBand(c *gin.Context) {
	var x domain.RainBand
	if !bind(c, &x) {
		return
	}
	out, err := h.App.Params.SaveRainBand(c.Request.Context(), x)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
func (h Handler) listRainBands(c *gin.Context) {
	out, err := h.App.Params.ListRainBands(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, out)
}
