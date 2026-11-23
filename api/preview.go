package api

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"

	"goqlprinter/brotherql"

	"github.com/gin-gonic/gin"
)

// PreviewRequest defines the structure for the preview request
type PreviewRequest struct {
	Text                string  `json:"text"`
	LabelSize           string  `json:"label_size" binding:"required"`
	FontFamily          string  `json:"font_family"`
	FontSize            float64 `json:"font_size"`
	Orientation         string  `json:"orientation"`
	HorizontalAlignment string  `json:"horizontal_alignment"`
	VerticalAlignment   string  `json:"vertical_alignment"`
	TextRotation        float64 `json:"text_rotation"`
	TextAlign           string  `json:"text_align"`
	SVGData             string  `json:"svg_data"`
	SVGScale            float64 `json:"svg_scale"`
	QRData              string  `json:"qr_data"`
	QRScale             float64 `json:"qr_scale"`
	PNGData             string  `json:"png_data"`
	PNGScale            float64 `json:"png_scale"`
	CustomHeightMM      float64 `json:"custom_height_mm"`
}

// PreviewResponse defines the response structure
type PreviewResponse struct {
	Image           string `json:"image"`            // Base64 data URL
	InkImage        string `json:"ink_image"`        // Base64 data URL of the same size as image: black where the printer burns a dot, white elsewhere
	Width           int    `json:"width"`            // Image width in pixels
	Height          int    `json:"height"`           // Image height in pixels
	PrintableWidth  int    `json:"printable_width"`  // Printable area width
	PrintableHeight int    `json:"printable_height"` // Printable area height
}

// PreviewLabel godoc
// @Summary Preview a label without printing
// @Description Renders a label preview and returns it as a base64-encoded PNG, with ink_image showing exactly the dots the printer will burn
// @Tags preview
// @Accept json
// @Produce json
// @Param request body PreviewRequest true "Preview request parameters"
// @Success 200 {object} PreviewResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /preview [post]
func (h *Handlers) PreviewLabel(c *gin.Context) {
	var req PreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	label, err := brotherql.GetLabel(req.LabelSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid label size"})
		return
	}

	var img *image.Gray

	switch {
	case req.SVGData != "":
		svgReq := PrintSVGRequest{
			LabelSize:           req.LabelSize,
			SVGData:             req.SVGData,
			SVGScale:            req.SVGScale,
			Orientation:         req.Orientation,
			ContentRotation:     req.TextRotation,
			HorizontalAlignment: req.HorizontalAlignment,
			VerticalAlignment:   req.VerticalAlignment,
			CustomHeightMM:      req.CustomHeightMM,
		}
		img, err = h.processSVG(c.Request.Context(), svgReq, label)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case req.QRData != "":
		qrReq := PrintQRRequest{
			LabelSize:           req.LabelSize,
			Data:                req.QRData,
			QRScale:             req.QRScale,
			Orientation:         req.Orientation,
			ContentRotation:     req.TextRotation,
			HorizontalAlignment: req.HorizontalAlignment,
			VerticalAlignment:   req.VerticalAlignment,
			CustomHeightMM:      req.CustomHeightMM,
		}
		img, err = processQR(qrReq, label)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case req.PNGData != "":
		pngReq := PrintPNGPayload{
			LabelSize:           req.LabelSize,
			PNGData:             req.PNGData,
			PNGScale:            req.PNGScale,
			Orientation:         req.Orientation,
			ContentRotation:     req.TextRotation,
			HorizontalAlignment: req.HorizontalAlignment,
			VerticalAlignment:   req.VerticalAlignment,
			CustomHeightMM:      req.CustomHeightMM,
		}
		img, err = processPNG(pngReq, label)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	default:
		printReq := PrintRequest{
			Text:                req.Text,
			LabelSize:           req.LabelSize,
			FontFamily:          req.FontFamily,
			FontSize:            req.FontSize,
			Orientation:         req.Orientation,
			HorizontalAlignment: req.HorizontalAlignment,
			VerticalAlignment:   req.VerticalAlignment,
			TextRotation:        req.TextRotation,
			TextAlign:           req.TextAlign,
			CustomHeightMM:      req.CustomHeightMM,
		}
		img, err = h.renderTextLabel(printReq, label)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	imagePNG, errImage := encodePNG(img)
	ink, errInk := inkPNG(img)
	if errImage != nil || errInk != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encode image"})
		return
	}

	c.JSON(http.StatusOK, PreviewResponse{
		Image:           pngDataURL(imagePNG),
		InkImage:        pngDataURL(ink),
		Width:           img.Bounds().Dx(),
		Height:          img.Bounds().Dy(),
		PrintableWidth:  label.DotsPrintableWidth,
		PrintableHeight: label.DotsPrintableHeight,
	})
}

// inkPNG is the ink_image PNG for a rendered label: the bytes a human
// approves and /api/print_svg_approved hashes.
func inkPNG(img *image.Gray) ([]byte, error) {
	return encodePNG(brotherql.InkMask(img))
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func pngDataURL(data []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}
