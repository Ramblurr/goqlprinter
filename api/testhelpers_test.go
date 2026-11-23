package api_test

import (
	"io"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	"goqlprinter/api"
	"goqlprinter/brotherql"
	"goqlprinter/internal/config"
	"goqlprinter/internal/services"
)

// TestMain sets gin to test mode once before any parallel test runs.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// mockProvider is a test double for brotherql.BackendProvider
type mockProvider struct {
	printers []brotherql.PrinterInfo
	err      error
}

func (m *mockProvider) FindPrinters() ([]brotherql.PrinterInfo, error) {
	return m.printers, m.err
}

func (m *mockProvider) Connect(printer brotherql.PrinterInfo) (brotherql.Backend, error) {
	return &mockBackend{}, nil
}

func (m *mockProvider) SupportsStatus() bool {
	return false
}

type mockBackend struct{}

func (m *mockBackend) Write(p []byte) (int, error) { return len(p), nil }
func (m *mockBackend) Read(p []byte) (int, error)  { return 0, io.EOF }
func (m *mockBackend) Close() error                { return nil }

// newTestHandlers creates a Handlers with minimal test dependencies.
func newTestHandlers(printers []brotherql.PrinterInfo) *api.Handlers {
	cfg := &config.Config{}
	cfg.Server.Port = 8000
	cfg.App.FontDirs = []string{}
	ps := services.NewPrinterService(&mockProvider{printers: printers})
	fs := services.NewFontService([]string{})
	return api.NewHandlers(ps, fs, nil, cfg)
}

// newSVGTestHandlers creates a Handlers that renders SVG with resvg, loading
// fonts only from fontDirs. It skips the test when resvg is not on PATH.
func newSVGTestHandlers(t *testing.T, fontDirs []string) *api.Handlers {
	t.Helper()
	svg, err := services.NewSVGRenderer(fontDirs)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	cfg := &config.Config{}
	ps := services.NewPrinterService(&mockProvider{})
	fs := services.NewFontService([]string{})
	return api.NewHandlers(ps, fs, svg, cfg)
}

// newTestHandlersWithError creates a Handlers whose provider always returns err.
func newTestHandlersWithError(err error) *api.Handlers {
	cfg := &config.Config{}
	ps := services.NewPrinterService(&mockProvider{err: err})
	fs := services.NewFontService([]string{})
	return api.NewHandlers(ps, fs, nil, cfg)
}
