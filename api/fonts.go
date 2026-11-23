package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"goqlprinter/internal/services"
)

// FontsResponse lists the text-mode font files and the font families that
// SVG text can use.
type FontsResponse struct {
	Fonts    []string              `json:"fonts"`
	Families []services.FontFamily `json:"families"`
}

// GetFonts godoc
// @Summary List available fonts
// @Description Returns the font file names for text labels and the catalog of font families usable as SVG font-family values
// @Tags fonts
// @Produce json
// @Success 200 {object} FontsResponse
// @Router /fonts [get]
func (h *Handlers) GetFonts(c *gin.Context) {
	fonts, err := h.Fonts.ListFonts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, FontsResponse{Fonts: fonts, Families: h.FontFamilies})
}
