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
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const komikaSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="106" viewBox="0 0 320 106">` +
	`<text x="10" y="80" font-family="Komika Axis" font-size="60">HELLO</text></svg>`

const blankSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="106" viewBox="0 0 320 106"></svg>`

func previewSVG(t *testing.T, r *gin.Engine, body map[string]any) *image.Gray {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/preview", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Image string `json:"image"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(resp.Image, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	gray, ok := img.(*image.Gray)
	if !ok {
		t.Fatalf("preview image is %T, want *image.Gray", img)
	}
	return gray
}

func inkPixels(img *image.Gray) int {
	n := 0
	for _, p := range img.Pix {
		if p < 128 {
			n++
		}
	}
	return n
}

// writeFontconfig points fontconfig, and so resvg's system font lookup, at a
// directory that holds only the Komika Axis test font.
func writeFontconfig(t *testing.T) {
	t.Helper()
	sysFonts := t.TempDir()
	font, err := os.ReadFile("../fonts/KOMIKAX_.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sysFonts, "KOMIKAX_.ttf"), font, 0o644); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(t.TempDir(), "fonts.conf")
	body := `<?xml version="1.0"?><fontconfig><dir>` + sysFonts + `</dir></fontconfig>`
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FONTCONFIG_FILE", conf)
}

func TestPreviewSVG_TextUsesOnlySVGFontDirs(t *testing.T) {
	writeFontconfig(t)

	cases := []struct {
		name     string
		fontDirs []string
		wantInk  bool
	}{
		{"family in svg_font_dirs", []string{"../fonts"}, true},
		{"family only installed as a system font", []string{t.TempDir()}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSVGTestHandlers(t, tc.fontDirs)
			r := gin.New()
			r.POST("/api/preview", h.PreviewLabel)

			without := previewSVG(t, r, map[string]any{"label_size": "62", "svg_data": blankSVG})
			if got := inkPixels(without); got != 0 {
				t.Fatalf("blank SVG has %d ink pixels, want 0", got)
			}

			withText := inkPixels(previewSVG(t, r, map[string]any{"label_size": "62", "svg_data": komikaSVG}))
			if tc.wantInk && withText < 500 {
				t.Fatalf("SVG text rendered %d ink pixels, want at least 500", withText)
			}
			if !tc.wantInk && withText != 0 {
				t.Fatalf("SVG text rendered %d ink pixels from a font outside svg_font_dirs, want 0", withText)
			}
		})
	}
}

func TestPreviewSVG_RotatedLaysLandscapeDesignAlongTape(t *testing.T) {
	t.Parallel()

	h := newSVGTestHandlers(t, nil)
	r := gin.New()
	r.POST("/api/preview", h.PreviewLabel)

	design := `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="106" viewBox="0 0 320 106">` +
		`<rect width="320" height="106" fill="black"/></svg>`
	img := previewSVG(t, r, map[string]any{"label_size": "12", "orientation": "rotated", "svg_data": design})

	if got := img.Bounds().Dy(); got != 106 {
		t.Fatalf("preview height = %d, want the 106 printable dots of 12 mm tape", got)
	}
	if got := inkPixels(img); got != 320*106 {
		t.Fatalf("ink pixels = %d, want the full 320x106 design", got)
	}
}
