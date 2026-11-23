package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// oneMetreDots is 1000 mm of tape at the printers' 300 dpi.
const oneMetreDots = 11811

func previewError(t *testing.T, r *gin.Engine, body map[string]any) (int, string) {
	t.Helper()
	w := postJSON(t, r, "/api/preview", body)
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("preview status %d: %v", w.Code, err)
	}
	return w.Code, resp.Error
}

func TestPreviewSVG_ContinuousTapeIsAtMostOneMetre(t *testing.T) {
	t.Parallel()

	h := newSVGTestHandlers(t, nil)
	r := gin.New()
	r.POST("/api/preview", h.PreviewLabel)

	tall := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="23582"><rect width="100" height="23582"/></svg>`
	wide := `<svg xmlns="http://www.w3.org/2000/svg" width="23582" height="100"><rect width="23582" height="100"/></svg>`

	if got := previewSVG(t, r, map[string]any{"label_size": "62", "svg_data": tall}).Bounds().Dy(); got != oneMetreDots {
		t.Errorf("standard preview of a 23582 px tall design is %d dots long, want %d (1 m)", got, oneMetreDots)
	}
	if got := previewSVG(t, r, map[string]any{"label_size": "12", "orientation": "rotated", "svg_data": wide}).Bounds().Dx(); got != oneMetreDots {
		t.Errorf("rotated preview of a 23582 px wide design is %d dots long, want %d (1 m)", got, oneMetreDots)
	}

	if got := previewSVG(t, r, map[string]any{"label_size": "62", "svg_data": blankSVG, "custom_height_mm": 1000}).Bounds().Dy(); got != oneMetreDots {
		t.Errorf("custom_height_mm 1000 preview is %d dots long, want %d", got, oneMetreDots)
	}
	code, msg := previewError(t, r, map[string]any{"label_size": "62", "svg_data": blankSVG, "custom_height_mm": 1001})
	if code == http.StatusOK || !strings.Contains(msg, "1000 mm") {
		t.Errorf("custom_height_mm 1001: status %d, error %q; want it refused for exceeding 1000 mm", code, msg)
	}
}

// fakeResvg puts a resvg script first on PATH. The script runs body and then
// the real resvg, which it finds as $REAL_RESVG.
func fakeResvg(t *testing.T, body string) {
	t.Helper()
	real, err := exec.LookPath("resvg")
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nREAL_RESVG=" + strconv.Quote(real) + "\n" + body + "\nexec \"$REAL_RESVG\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "resvg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPreviewSVG_RunsAtMostTwoRenderersAtOnce(t *testing.T) {
	running := t.TempDir()
	log := filepath.Join(t.TempDir(), "concurrency.log")
	fakeResvg(t, `touch "`+running+`/$$"; sleep 0.3; ls "`+running+`" | wc -l >> "`+log+`"; rm "`+running+`/$$"`)
	h := newSVGTestHandlers(t, nil)
	r := gin.New()
	r.POST("/api/preview", h.PreviewLabel)

	const requests = 6
	var wg sync.WaitGroup
	codes := make([]int, requests)
	for i := range requests {
		wg.Go(func() {
			codes[i] = postJSON(t, r, "/api/preview", map[string]any{"label_size": "12", "svg_data": blankSVG}).Code
		})
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, code)
		}
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	counts := strings.Fields(string(data))
	if len(counts) != requests {
		t.Fatalf("resvg ran %d times, want %d", len(counts), requests)
	}
	for _, c := range counts {
		if n, _ := strconv.Atoi(c); n > 2 {
			t.Fatalf("%d resvg processes ran at once (samples %v), want at most 2", n, counts)
		}
	}
}

func TestPreviewSVG_BoundsCapturedResvgOutput(t *testing.T) {
	cases := []struct {
		name, body, want string
		maxError         int
	}{
		{"stderr", `head -c 4194304 /dev/zero | tr '\0' x >&2; exit 1`, "resvg failed", 32 << 10},
		{"stdout", `head -c 4194304 /dev/zero; exit 0`, "resvg output exceeds", 1 << 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeResvg(t, tc.body)
			h := newSVGTestHandlers(t, nil)
			r := gin.New()
			r.POST("/api/preview", h.PreviewLabel)

			code, msg := previewError(t, r, map[string]any{"label_size": "12", "svg_data": blankSVG})
			if code == http.StatusOK || !strings.Contains(msg, tc.want) {
				t.Fatalf("status %d, error %.200q; want an error containing %q", code, msg, tc.want)
			}
			if len(msg) > tc.maxError {
				t.Fatalf("error is %d bytes, want at most %d", len(msg), tc.maxError)
			}
		})
	}
}
