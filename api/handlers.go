// Package api provides the HTTP handler layer for the Brother QL label printer web service.
package api

import (
	"goqlprinter/internal/config"
	"goqlprinter/internal/services"
)

// Handlers holds all dependencies for the API layer.
type Handlers struct {
	Printers *services.PrinterService
	Fonts    *services.FontService
	SVG      *services.SVGRenderer
	Config   *config.Config
}

// NewHandlers creates a new Handlers with all dependencies injected.
func NewHandlers(ps *services.PrinterService, fs *services.FontService, svg *services.SVGRenderer, cfg *config.Config) *Handlers {
	return &Handlers{Printers: ps, Fonts: fs, SVG: svg, Config: cfg}
}
