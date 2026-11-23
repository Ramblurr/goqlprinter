package api_test

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"goqlprinter/brotherql"
)

// brotherQLOffsetR is label.offset_r from brother_ql's labels.py, the dots
// left blank between the label and the right edge of the print head.
var brotherQLOffsetR = map[string]int{
	"12":    29,
	"29":    6,
	"62":    12,
	"29x90": 6,
}

// brotherQLAdditionalOffsetR is model.additional_offset_r from brother_ql's
// models.py.
var brotherQLAdditionalOffsetR = map[string]int{
	"QL-810W": 0,
	"QL-1100": 44,
}

var brotherQLBytesPerRow = map[string]int{
	"QL-810W": 90,
	"QL-1100": 162,
}

func TestPrintPNGRaw_InkLandsWhereBrotherQLPlacesIt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ model, label string }{
		{"QL-810W", "12"},
		{"QL-810W", "29"},
		{"QL-810W", "62"},
		{"QL-810W", "29x90"},
		{"QL-1100", "62"},
	} {
		t.Run(tc.model+" "+tc.label, func(t *testing.T) {
			t.Parallel()

			label, err := brotherql.GetLabel(tc.label)
			if err != nil {
				t.Fatal(err)
			}
			width, height := label.DotsPrintableWidth, label.DotsPrintableHeight
			if height == 0 {
				height = 40
			}

			listener, received := listenForPrintJob(t)
			h := newTestHandlersWithProvider(brotherql.NewTCPProvider(listener.Addr().String(), tc.model))
			r := gin.New()
			r.POST("/api/print_png_raw", h.PrintPNGRaw)
			if w := postBlackPNG(t, r, tc.model, tc.label, width, height); w.Code != http.StatusOK {
				t.Fatalf("print_png_raw status = %d, body: %s", w.Code, w.Body.String())
			}

			// brother_ql pastes the image at device_pixel_width - width -
			// right_margin_dots, then mirrors each row before sending it.
			headWidth := brotherQLBytesPerRow[tc.model] * 8
			rightMargin := brotherQLOffsetR[tc.label] + brotherQLAdditionalOffsetR[tc.model]
			pasteX := headWidth - width - rightMargin
			firstBit, lastBit := headWidth-1-(pasteX+width-1), headWidth-1-pasteX

			rows := rasterRows(t, <-received, brotherQLBytesPerRow[tc.model])
			if len(rows) != height {
				t.Fatalf("printer received %d rows, want %d", len(rows), height)
			}
			for y, row := range rows {
				for bit := range headWidth {
					dot := row[bit/8]&(0x80>>(bit%8)) != 0
					if want := bit >= firstBit && bit <= lastBit; dot != want {
						t.Fatalf("row %d head dot %d printed=%v; want ink on dots %d..%d", y, bit, dot, firstBit, lastBit)
					}
				}
			}
		})
	}
}

func TestPrintPNGRaw_LabelWiderThanTheHeadFails(t *testing.T) {
	t.Parallel()

	listener, received := listenForPrintJob(t)
	h := newTestHandlersWithProvider(brotherql.NewTCPProvider(listener.Addr().String(), "QL-810W"))
	r := gin.New()
	r.POST("/api/print_png_raw", h.PrintPNGRaw)

	w := postBlackPNG(t, r, "QL-810W", "102", 1164, 40)
	if w.Code != http.StatusInternalServerError || !bytes.Contains(w.Body.Bytes(), []byte("raster")) {
		t.Fatalf("print_png_raw status = %d, body: %s; want 500 naming the raster width", w.Code, w.Body.String())
	}
	_ = listener.Close()
	if stream := <-received; len(stream) != 0 {
		t.Fatalf("printer received %d bytes for a label wider than the head; want none", len(stream))
	}
}

func postBlackPNG(t *testing.T, r *gin.Engine, printer, labelSize string, width, height int) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for field, value := range map[string]string{"printer": printer, "label_size": labelSize} {
		if err := form.WriteField(field, value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := form.CreateFormFile("file", "black.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/print_png_raw", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	r.ServeHTTP(w, req)
	return w
}
