package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"goqlprinter/api"
	"goqlprinter/brotherql"
)

func TestPrintSVGApproved_PrintsTheApprovedInk(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, label, orientation string
	}{
		{"standard 62mm", "62", ""},
		{"rotated 12mm", "12", "rotated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			listener, received := listenForPrintJob(t)
			r := approvedRouter(newSVGPrintHandlers(t, brotherql.NewTCPProvider(listener.Addr().String(), parityModel)))

			body := map[string]any{"label_size": tc.label, "orientation": tc.orientation, "svg_data": paritySVG}
			approvedPNG := previewInkPNG(t, r, body)
			approved := decodeGrayPNG(t, approvedPNG)

			body["printer"] = parityModel
			body["approved_ink_sha256"] = sha256Hex(approvedPNG)
			if w := postJSON(t, r, "/api/print_svg_approved", body); w.Code != http.StatusOK {
				t.Fatalf("print_svg_approved status = %d, body: %s", w.Code, w.Body.String())
			}
			printed := decodePrintedInk(t, <-received, approved.Bounds(), tc.orientation == "rotated", tc.label)

			if !printed.Rect.Eq(approved.Rect) {
				t.Fatalf("printed ink is %v, approved ink_image is %v", printed.Rect, approved.Rect)
			}
			for y := range approved.Rect.Dy() {
				for x := range approved.Rect.Dx() {
					if got, want := printed.GrayAt(x, y).Y, approved.GrayAt(x, y).Y; got != want {
						t.Fatalf("pixel (%d,%d): printer received %d, approved ink_image shows %d", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestPrintSVGApproved_RefusesInkThatWasNotApproved(t *testing.T) {
	t.Parallel()

	listener, received := listenForPrintJob(t)
	r := approvedRouter(newSVGPrintHandlers(t, brotherql.NewTCPProvider(listener.Addr().String(), parityModel)))

	otherInk := sha256Hex(previewInkPNG(t, r, map[string]any{"label_size": "62", "svg_data": blankSVG}))
	currentInk := sha256Hex(previewInkPNG(t, r, map[string]any{"label_size": "62", "svg_data": paritySVG}))

	w := postJSON(t, r, "/api/print_svg_approved", map[string]any{
		"label_size":          "62",
		"svg_data":            paritySVG,
		"printer":             parityModel,
		"approved_ink_sha256": otherInk,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
	var resp struct {
		Error    string `json:"error"`
		Approved string `json:"approved_ink_sha256"`
		Current  string `json:"current_ink_sha256"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == "" || resp.Approved != otherInk || resp.Current != currentInk {
		t.Fatalf("409 body = %+v, want an error with approved %s and current %s", resp, otherInk, currentInk)
	}

	_ = listener.Close()
	if data := <-received; data != nil {
		t.Fatalf("printer received %d bytes for an unapproved label", len(data))
	}
}

func TestPrintSVGApproved_RejectsMissingOrMalformedHash(t *testing.T) {
	t.Parallel()

	valid := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		name string
		hash any
	}{
		{"missing", nil},
		{"empty", ""},
		{"uppercase", strings.ToUpper(valid)},
		{"too short", valid[:63]},
		{"too long", valid + "a"},
		{"not hex", strings.Repeat("g", 64)},
		{"0x prefixed", "0x" + valid[:62]},
		{"not a string", 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := approvedRouter(newSVGTestHandlers(t, nil))
			body := map[string]any{"label_size": "62", "svg_data": paritySVG}
			if tc.hash != nil {
				body["approved_ink_sha256"] = tc.hash
			}
			if w := postJSON(t, r, "/api/print_svg_approved", body); w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
			}
		})
	}
}

func TestAPIRoutes_LimitRequestBodiesTo8MiB(t *testing.T) {
	t.Parallel()

	r := gin.New()
	routes := r.Group("/api", api.LimitRequestBody(api.MaxRequestBodyBytes))
	routes.POST("/print_svg_approved", newSVGTestHandlers(t, nil).PrintSVGApproved)

	for _, tc := range []struct {
		name     string
		size     int
		want     int
		wantBody string
	}{
		{"at the limit reaches the handler", 8 << 20, http.StatusBadRequest, "approved_ink_sha256"},
		{"one byte over", 8<<20 + 1, http.StatusRequestEntityTooLarge, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/print_svg_approved", bytes.NewReader(paddedApprovalBody(t, tc.size)))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tc.want || !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Fatalf("status = %d, body: %.200s; want %d mentioning %q", w.Code, w.Body.String(), tc.want, tc.wantBody)
			}
		})
	}
}

func TestPreviewInkImage_IsDeterministic(t *testing.T) {
	t.Parallel()

	r := approvedRouter(newSVGTestHandlers(t, nil))
	body := map[string]any{"label_size": "62", "svg_data": paritySVG}
	first, second := previewInkPNG(t, r, body), previewInkPNG(t, r, body)
	if !bytes.Equal(first, second) {
		t.Fatalf("two previews of one request gave ink_image %s and %s", sha256Hex(first), sha256Hex(second))
	}
}

func approvedRouter(h *api.Handlers) *gin.Engine {
	r := gin.New()
	r.POST("/api/preview", h.PreviewLabel)
	r.POST("/api/print_svg_approved", h.PrintSVGApproved)
	return r
}

// paddedApprovalBody is a print_svg_approved JSON body of exactly size bytes
// whose malformed hash the handler rejects once it reads the body.
func paddedApprovalBody(t *testing.T, size int) []byte {
	t.Helper()
	const head, tail = `{"label_size":"62","approved_ink_sha256":"not-a-hash","svg_data":"`, `"}`
	pad := size - len(head) - len(tail)
	if pad < 0 {
		t.Fatalf("size %d is below the %d byte JSON frame", size, len(head)+len(tail))
	}
	return []byte(head + strings.Repeat(" ", pad) + tail)
}

func previewInkPNG(t *testing.T, r *gin.Engine, body map[string]any) []byte {
	t.Helper()
	w := postJSON(t, r, "/api/preview", body)
	if w.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		InkImage string `json:"ink_image"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(resp.InkImage, prefix) {
		t.Fatalf("ink_image = %.40q, want a %s URL", resp.InkImage, prefix)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(resp.InkImage, prefix))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeGrayPNG(t *testing.T, data []byte) *image.Gray {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	gray, ok := img.(*image.Gray)
	if !ok {
		t.Fatalf("ink_image is %T, want *image.Gray", img)
	}
	return gray
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
