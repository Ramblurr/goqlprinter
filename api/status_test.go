package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"goqlprinter/api"
	"goqlprinter/brotherql"
	"goqlprinter/internal/config"
	"goqlprinter/internal/services"
)

type statusProvidingBackend struct {
	status brotherql.PrinterStatus
	writes int
}

func (b *statusProvidingBackend) Write(p []byte) (int, error) { b.writes++; return len(p), nil }
func (b *statusProvidingBackend) Read(p []byte) (int, error)  { return 0, io.EOF }
func (b *statusProvidingBackend) Close() error                { return nil }
func (b *statusProvidingBackend) IPPStatus() (brotherql.PrinterStatus, error) {
	return b.status, nil
}

type singleBackendProvider struct {
	backend brotherql.Backend
}

func (p *singleBackendProvider) FindPrinters() ([]brotherql.PrinterInfo, error) {
	return []brotherql.PrinterInfo{{Name: "QL-810W", Model: "QL-810W", URI: "tcp://printer:9100", Backend: "tcp"}}, nil
}
func (p *singleBackendProvider) Connect(brotherql.PrinterInfo) (brotherql.Backend, error) {
	return p.backend, nil
}
func (p *singleBackendProvider) SupportsStatus() bool { return true }

func TestGetStatus_UsesBackendIPPStatus(t *testing.T) {
	want := brotherql.PrinterStatus{
		Ready: true, ModelName: "QL-810W",
		MediaType: "Continuous length tape", MediaWidth: 12,
		StatusType: "Reply to status request", PhaseType: "Waiting to receive",
		Errors: []string{},
	}
	backend := &statusProvidingBackend{status: want}
	h := api.NewHandlers(
		services.NewPrinterService(&singleBackendProvider{backend: backend}),
		services.NewFontService([]string{}),
		nil,
		&config.Config{},
	)
	r := gin.New()
	r.POST("/api/status", h.GetStatus)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/status", strings.NewReader(`{"printer":"QL-810W"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var got api.StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Status, want) {
		t.Errorf("status =\n%+v\nwant\n%+v", got.Status, want)
	}
	if backend.writes != 0 {
		t.Errorf("handler wrote %d raw commands to a backend that reports status itself", backend.writes)
	}
}

type osStatusBackend struct{ writes int }

func (b *osStatusBackend) Write(p []byte) (int, error) { b.writes++; return len(p), nil }
func (b *osStatusBackend) Read(p []byte) (int, error)  { return 0, io.EOF }
func (b *osStatusBackend) Close() error                { return nil }
func (b *osStatusBackend) GetStatus() (brotherql.PrinterStatus, error) {
	return brotherql.PrinterStatus{Ready: true}, nil
}

func TestGetStatus_KeepsRawStatusRequestForOSStatusProviders(t *testing.T) {
	backend := &osStatusBackend{}
	h := api.NewHandlers(
		services.NewPrinterService(&singleBackendProvider{backend: backend}),
		services.NewFontService([]string{}),
		nil,
		&config.Config{},
	)
	r := gin.New()
	r.POST("/api/status", h.GetStatus)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/status", strings.NewReader(`{"printer":"QL-810W"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if backend.writes != 1 {
		t.Errorf("handler sent %d raw status requests, want 1", backend.writes)
	}
}
