package api_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func blackPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 40, 40))
	for i := range img.Pix {
		img.Pix[i] = 0
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPreviewSVG_RefusesReferencesToServerFiles(t *testing.T) {
	t.Parallel()

	h := newSVGTestHandlers(t, nil)
	r := gin.New()
	r.POST("/api/preview", h.PreviewLabel)

	secret := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(secret, blackPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, svg := range map[string]string{
		"image href": `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><image href="` + secret + `" width="40" height="40"/></svg>`,
		"image xlink:href": `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="40" height="40">` +
			`<image xlink:href="` + secret + `" width="40" height="40"/></svg>`,
		"relative image": `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><image href="secret.png" width="40" height="40"/></svg>`,
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"label_size": "12", "svg_data": svg})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/preview", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code == http.StatusOK {
				t.Fatalf("preview of an SVG referencing %s succeeded; want it refused", secret)
			}
		})
	}
}

func TestPreviewSVG_EmbedsDataURIImages(t *testing.T) {
	t.Parallel()

	h := newSVGTestHandlers(t, nil)
	r := gin.New()
	r.POST("/api/preview", h.PreviewLabel)

	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(blackPNG(t))
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><image href="` + uri + `" width="40" height="40"/></svg>`
	img := previewSVG(t, r, map[string]any{"label_size": "12", "svg_data": svg})
	if inkPixels(img) == 0 {
		t.Fatal("embedded data: URI image rendered no ink")
	}
}
