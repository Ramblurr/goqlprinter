package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"goqlprinter/brotherql"
)

var approvedInkHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// PrintSVGApprovedRequest is a PrintSVGRequest bound to the ink a human approved.
// @description Request body for printing an SVG label only if its ink matches an approved preview
type PrintSVGApprovedRequest struct {
	PrintSVGRequest
	// Lowercase hex SHA-256 of the PNG bytes in /api/preview's ink_image for the same request
	ApprovedInkSHA256 string `json:"approved_ink_sha256" binding:"required"`
}

// InkMismatchResponse is returned when the label no longer renders to the approved ink.
type InkMismatchResponse struct {
	Error             string `json:"error"`
	ApprovedInkSHA256 string `json:"approved_ink_sha256"`
	CurrentInkSHA256  string `json:"current_ink_sha256"`
}

// PrintSVGApproved godoc
// @Summary Print an approved SVG label
// @Description Renders the SVG once and prints it only if the SHA-256 of its ink_image PNG equals approved_ink_sha256. Preview with the same fields; preview's text_rotation is this request's content_rotation.
// @Tags print
// @Accept json
// @Produce json
// @Param request body PrintSVGApprovedRequest true "SVG print request with the approved ink hash"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 409 {object} InkMismatchResponse
// @Failure 413 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /print_svg_approved [post]
func (h *Handlers) PrintSVGApproved(c *gin.Context) {
	var req PrintSVGApprovedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !approvedInkHash.MatchString(req.ApprovedInkSHA256) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "approved_ink_sha256 must be 64 lowercase hex characters"})
		return
	}

	label, err := brotherql.GetLabel(req.LabelSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid label size"})
		return
	}

	img, err := h.processSVG(c.Request.Context(), req.PrintSVGRequest, label)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ink, err := inkPNG(img)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encode image"})
		return
	}
	sum := sha256.Sum256(ink)
	if current := hex.EncodeToString(sum[:]); current != req.ApprovedInkSHA256 {
		c.JSON(http.StatusConflict, InkMismatchResponse{
			Error:             "label ink differs from the approved preview; preview it again and get a new approval",
			ApprovedInkSHA256: req.ApprovedInkSHA256,
			CurrentInkSHA256:  current,
		})
		return
	}

	h.printSVGLabel(c, req.PrintSVGRequest, img, label)
}
