package api_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"goqlprinter/api"
	"goqlprinter/brotherql"
	"goqlprinter/internal/config"
	"goqlprinter/internal/services"
)

const (
	parityModel      = "QL-810W"
	parityRasterBits = 90 * 8
)

// paritySVG mixes a faint #f8f8f8 fill, which prints, a #fafafa fill, which
// does not, and antialiased edges whose grays straddle the print threshold.
const paritySVG = `<svg xmlns="http://www.w3.org/2000/svg" width="240" height="90" viewBox="0 0 240 90">` +
	`<rect x="0" y="0" width="60" height="90" fill="#f8f8f8"/>` +
	`<rect x="60" y="0" width="60" height="90" fill="#fafafa"/>` +
	`<circle cx="160" cy="45" r="30" fill="black"/>` +
	`<path d="M195 85 L235 5" stroke="#808080" stroke-width="3"/>` +
	`</svg>`

type previewImages struct {
	image, ink *image.Gray
}

func TestPreviewInkImage_IsWhatThePrinterReceives(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, label, orientation string
	}{
		{"standard 62mm", "62", ""},
		{"rotated 12mm", "12", "rotated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			received := make(chan []byte, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					received <- nil
					return
				}
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
				data, _ := io.ReadAll(conn)
				received <- data
			}()

			r := gin.New()
			h := newSVGPrintHandlers(t, brotherql.NewTCPProvider(listener.Addr().String(), parityModel))
			r.POST("/api/preview", h.PreviewLabel)
			r.POST("/api/print_svg", h.PrintSVG)

			body := map[string]any{"label_size": tc.label, "orientation": tc.orientation, "svg_data": paritySVG}
			preview := previewWithInk(t, r, body)

			body["printer"] = parityModel
			if w := postJSON(t, r, "/api/print_svg", body); w.Code != http.StatusOK {
				t.Fatalf("print_svg status = %d, body: %s", w.Code, w.Body.String())
			}
			printed := decodePrintedInk(t, <-received, preview.ink.Bounds(), tc.orientation == "rotated")

			if !printed.Rect.Eq(preview.ink.Rect) {
				t.Fatalf("printed ink is %v, ink_image is %v", printed.Rect, preview.ink.Rect)
			}
			for y := range preview.ink.Rect.Dy() {
				for x := range preview.ink.Rect.Dx() {
					if got, want := printed.GrayAt(x, y).Y, preview.ink.GrayAt(x, y).Y; got != want {
						t.Fatalf("pixel (%d,%d): printer received %d, ink_image shows %d", x, y, got, want)
					}
				}
			}

			faint, faintest := faintFillPixels(t, preview)
			if preview.image.GrayAt(faint.X, faint.Y).Y != 0xf8 || preview.ink.GrayAt(faint.X, faint.Y).Y != 0 {
				t.Fatalf("#f8f8f8 fill at %v: image %d, ink %d; want 248 shown as ink",
					faint, preview.image.GrayAt(faint.X, faint.Y).Y, preview.ink.GrayAt(faint.X, faint.Y).Y)
			}
			if preview.image.GrayAt(faintest.X, faintest.Y).Y != 0xfa || preview.ink.GrayAt(faintest.X, faintest.Y).Y != 0xff {
				t.Fatalf("#fafafa fill at %v: image %d, ink %d; want 250 shown as no ink",
					faintest, preview.image.GrayAt(faintest.X, faintest.Y).Y, preview.ink.GrayAt(faintest.X, faintest.Y).Y)
			}
		})
	}
}

// faintFillPixels returns a point inside each of paritySVG's two faint
// rectangles, found through the preview's layout of the design.
func faintFillPixels(t *testing.T, p previewImages) (image.Point, image.Point) {
	t.Helper()
	b := p.image.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if p.image.GrayAt(x, y).Y == 0xf8 {
				// The design is drawn at its natural size, so the #fafafa
				// rectangle starts 60 px after the #f8f8f8 one.
				return image.Pt(x+5, y+5), image.Pt(x+65, y+5)
			}
		}
	}
	t.Fatal("preview has no #f8f8f8 pixel")
	return image.Point{}, image.Point{}
}

func newSVGPrintHandlers(t *testing.T, provider brotherql.BackendProvider) *api.Handlers {
	t.Helper()
	svg, err := services.NewSVGRenderer(nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	return api.NewHandlers(services.NewPrinterService(provider), services.NewFontService(nil), svg, nil, &config.Config{})
}

func postJSON(t *testing.T, r *gin.Engine, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func previewWithInk(t *testing.T, r *gin.Engine, body map[string]any) previewImages {
	t.Helper()
	w := postJSON(t, r, "/api/preview", body)
	if w.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Image    string `json:"image"`
		InkImage string `json:"ink_image"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	p := previewImages{image: decodeGrayDataURL(t, "image", resp.Image), ink: decodeGrayDataURL(t, "ink_image", resp.InkImage)}
	if !p.image.Rect.Eq(p.ink.Rect) {
		t.Fatalf("ink_image is %v, image is %v", p.ink.Rect, p.image.Rect)
	}
	return p
}

func decodeGrayDataURL(t *testing.T, field, url string) *image.Gray {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("%s = %.40q, want a %s URL", field, url, prefix)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, prefix))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s: %v", field, err)
	}
	gray, ok := img.(*image.Gray)
	if !ok {
		t.Fatalf("%s is %T, want *image.Gray", field, img)
	}
	return gray
}

// decodePrintedInk turns the raster rows of a Brother QL job back into an
// image in the preview's reading orientation: it unpacks PackBits rows,
// undoes the protocol's horizontal mirror, drops the padding to the full
// raster width, and undoes the 90 degree counter-clockwise turn applied to
// rotated labels. Printed dots are black, the rest white.
func decodePrintedInk(t *testing.T, stream []byte, want image.Rectangle, rotated bool) *image.Gray {
	t.Helper()
	rows := rasterRows(t, stream)

	headDots := want.Dx()
	if rotated {
		headDots = want.Dy()
	}
	pad := parityRasterBits - headDots
	sent := image.NewGray(image.Rect(0, 0, headDots, len(rows)))
	for y, row := range rows {
		for bit := range parityRasterBits {
			dot := row[bit/8]&(0x80>>(bit%8)) != 0
			x := parityRasterBits - 1 - bit - pad
			if x < 0 {
				if dot {
					t.Fatalf("row %d prints raster bit %d, outside the %d dots of the label", y, bit, headDots)
				}
				continue
			}
			if !dot {
				sent.Pix[y*sent.Stride+x] = 0xff
			}
		}
	}
	if !rotated {
		return sent
	}

	out := image.NewGray(image.Rect(0, 0, sent.Rect.Dy(), sent.Rect.Dx()))
	for y := range out.Rect.Dy() {
		for x := range out.Rect.Dx() {
			out.Pix[y*out.Stride+x] = sent.GrayAt(y, out.Rect.Dx()-1-x).Y
		}
	}
	return out
}

func rasterRows(t *testing.T, stream []byte) [][]byte {
	t.Helper()
	start := bytes.Index(stream, []byte{0x1b, 0x69, 0x4b, 0x08})
	if start < 0 {
		t.Fatalf("no ESC i K in %d received bytes", len(stream))
	}
	i := start + 4
	if stream[i] != 'M' {
		t.Fatalf("byte %d = %#x, want the compression command", i, stream[i])
	}
	compressed := stream[i+1] == 0x02
	i += 2
	var rows [][]byte
	for stream[i] == 'g' {
		n := int(stream[i+2])
		data := stream[i+3 : i+3+n]
		if compressed {
			data = unpackBits(data)
		}
		if len(data) != parityRasterBits/8 {
			t.Fatalf("raster row %d has %d bytes, want %d", len(rows), len(data), parityRasterBits/8)
		}
		rows = append(rows, data)
		i += 3 + n
	}
	if stream[i] != 0x1a {
		t.Fatalf("raster rows end with %#x, want print command 0x1a", stream[i])
	}
	return rows
}

func unpackBits(data []byte) []byte {
	var out []byte
	for i := 0; i < len(data); {
		n := int(data[i])
		i++
		switch {
		case n < 128:
			out = append(out, data[i:i+n+1]...)
			i += n + 1
		case n > 128:
			out = append(out, bytes.Repeat(data[i:i+1], 257-n)...)
			i++
		}
	}
	return out
}
