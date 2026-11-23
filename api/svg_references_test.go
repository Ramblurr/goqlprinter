package api_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	for _, tc := range []struct{ name, href, svg string }{
		{"image href", secret, `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><image href="` + secret + `" width="40" height="40"/></svg>`},
		{"image xlink:href", secret, `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="40" height="40">` +
			`<image xlink:href="` + secret + `" width="40" height="40"/></svg>`},
		{"relative image", "secret.png", `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><image href="secret.png" width="40" height="40"/></svg>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := postJSON(t, r, "/api/preview", map[string]any{"label_size": "12", "svg_data": tc.svg})
			var resp struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("preview status %d, body %s: %v", w.Code, w.Body.String(), err)
			}
			want := fmt.Sprintf("SVG references %q; only #fragment and data: references are allowed", tc.href)
			if w.Code == http.StatusOK || !strings.Contains(resp.Error, want) {
				t.Fatalf("preview status %d, error %q; want the reference refused with %q", w.Code, resp.Error, want)
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

// Fragment and data: references pass the reference check, so resvg must not
// resolve them as paths relative to the server's working directory.
func TestPreviewSVG_DoesNotLoadAllowedReferencesAsFiles(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	h := newSVGTestHandlers(t, nil)
	r := gin.New()
	r.POST("/api/preview", h.PreviewLabel)

	for _, href := range []string{"#ink.png", "data:ink.png"} {
		t.Run(href, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(cwd, href), blackPNG(t), 0o644); err != nil {
				t.Fatal(err)
			}
			svg := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><image href="` + href + `" width="40" height="40"/></svg>`
			if got := inkPixels(previewSVG(t, r, map[string]any{"label_size": "12", "svg_data": svg})); got != 0 {
				t.Fatalf("SVG image %q rendered %d ink pixels from a file in the working directory, want 0", href, got)
			}
		})
	}
}
