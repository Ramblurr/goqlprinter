package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"goqlprinter/internal/services"
)

func TestGetFonts_Empty(t *testing.T) {
	t.Parallel()

	// newTestHandlers uses empty FontDirs, so no fonts will be discovered.
	h := newTestHandlers(nil)
	r := gin.New()
	r.GET("/api/fonts", h.GetFonts)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/fonts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"fonts"`) {
		t.Errorf("body missing 'fonts' key: %s", body)
	}
	// fonts list must be present; when no font dirs are configured the handler
	// returns a nil slice which JSON-encodes as null.
	if !strings.Contains(body, `"fonts":[]`) && !strings.Contains(body, `"fonts":null`) {
		t.Errorf("expected fonts key with empty/null value, got: %s", body)
	}
}

func TestGetFonts_IncludesCatalogFamilies(t *testing.T) {
	t.Parallel()

	h := newTestHandlers(nil)
	h.FontFamilies = []services.FontFamily{
		{Family: "Oswald", Source: "google-fonts", Category: []string{"SANS_SERIF"}, Weights: []int{400},
			Axes: []services.FontAxis{{Tag: "wght", Min: 200, Max: 700}}},
	}
	r := gin.New()
	r.GET("/api/fonts", h.GetFonts)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/fonts", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Families []services.FontFamily `json:"families"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Families) != 1 || resp.Families[0].Family != "Oswald" || resp.Families[0].Axes[0].Max != 700 {
		t.Fatalf("families = %#v", resp.Families)
	}
}

func TestGetFonts_NoCatalogMeansEmptyFamilies(t *testing.T) {
	t.Parallel()

	h := newTestHandlers(nil)
	r := gin.New()
	r.GET("/api/fonts", h.GetFonts)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/fonts", nil))

	if !strings.Contains(w.Body.String(), `"families":[]`) {
		t.Fatalf("body = %s, want an empty families array", w.Body.String())
	}
}
