package brotherql

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	ipp "github.com/phin1x/go-ipp"
)

func ippAttrs(values map[string][]any) ipp.Attributes {
	attrs := ipp.Attributes{}
	for name, vals := range values {
		for _, v := range vals {
			attrs[name] = append(attrs[name], ipp.Attribute{Name: name, Value: v})
		}
	}
	return attrs
}

func TestStatusFromIPP(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string][]any
		want  PrinterStatus
	}{
		{
			name: "idle with continuous tape",
			attrs: map[string][]any{
				"printer-state":          {3},
				"printer-state-reasons":  {"none"},
				"media-ready":            {"roll_current_12x0mm"},
				"printer-make-and-model": {"Brother QL-810W"},
			},
			want: PrinterStatus{
				Ready: true, ModelName: "QL-810W",
				MediaType: "Continuous length tape", MediaWidth: 12,
				StatusType: "Reply to status request", PhaseType: "Waiting to receive",
				Errors: []string{},
			},
		},
		{
			name: "processing with die-cut labels",
			attrs: map[string][]any{
				"printer-state":          {4},
				"media-ready":            {"roll_current_62x29mm"},
				"printer-make-and-model": {"Brother QL-820NWB"},
			},
			want: PrinterStatus{
				Busy: true, ModelName: "QL-820NWB",
				MediaType: "Die-cut labels", MediaWidth: 62, MediaLength: 29,
				StatusType: "Reply to status request", PhaseType: "Printing state",
				Errors: []string{},
			},
		},
		{
			name: "stopped with known and unknown errors, reports and warnings ignored",
			attrs: map[string][]any{
				"printer-state": {5},
				"printer-state-reasons": {
					"media-empty-error", "cover-open", "toner-low-warning",
					"connecting-to-device-report", "media-jam-error", "spool-area-full",
				},
			},
			want: PrinterStatus{
				Error:      "End of media",
				MediaType:  "No media",
				StatusType: "Reply to status request",
				Errors: []string{
					"End of media", "Cover opened while printing",
					"Media cannot be fed", "spool-area-full",
				},
			},
		},
		{
			name: "stopped without a reason is still an error",
			attrs: map[string][]any{
				"printer-state": {5},
				"media-ready":   {""},
			},
			want: PrinterStatus{
				Error: "stopped", MediaType: "No media",
				StatusType: "Reply to status request",
				Errors:     []string{"stopped"},
			},
		},
		{
			name: "idle with an error reason is not ready",
			attrs: map[string][]any{
				"printer-state":         {3},
				"printer-state-reasons": {"media-needed", "door-open-error"},
			},
			want: PrinterStatus{
				Error: "No media when printing", MediaType: "No media",
				StatusType: "Reply to status request", PhaseType: "Waiting to receive",
				Errors: []string{"No media when printing", "Cover opened while printing"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusFromIPP(ippAttrs(tt.attrs))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("statusFromIPP() =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestQueryIPPStatusOverHTTP(t *testing.T) {
	// go-ipp encodes only attributes in its tag mapping, and media-ready is not one.
	ipp.AttributeTagMapping["media-ready"] = ipp.TagKeyword
	defer delete(ipp.AttributeTagMapping, "media-ready")

	var gotOp int16
	var gotPrinterURI any
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotBody = body
		req, err := ipp.NewRequestDecoder(bytes.NewReader(body)).Decode(nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotOp = req.Operation
		gotPrinterURI = req.OperationAttributes[ipp.AttributePrinterURI]

		resp := ipp.NewResponse(ipp.StatusOk, req.RequestId)
		resp.PrinterAttributes = append(resp.PrinterAttributes, ippAttrs(map[string][]any{
			"printer-state":          {3},
			"printer-state-reasons":  {"none"},
			"media-ready":            {"roll_current_29x0mm"},
			"printer-make-and-model": {"Brother QL-810W"},
		}))
		out, err := resp.Encode()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", ipp.ContentTypeIPP)
		_, _ = w.Write(out)
	}))
	defer server.Close()

	got, err := queryIPPStatus(server.URL + "/ipp/print")
	if err != nil {
		t.Fatal(err)
	}
	if gotOp != ipp.OperationGetPrinterAttributes {
		t.Errorf("operation = %#x, want Get-Printer-Attributes", gotOp)
	}
	if want := "ipp://" + strings.TrimPrefix(server.URL, "http://") + "/ipp/print"; gotPrinterURI != want {
		t.Errorf("printer-uri = %v, want %s", gotPrinterURI, want)
	}
	for _, name := range []string{"printer-state", "printer-state-reasons", "media-ready", "printer-make-and-model"} {
		if !bytes.Contains(gotBody, []byte(name)) {
			t.Errorf("request does not ask for %s", name)
		}
	}
	want := PrinterStatus{
		Ready: true, ModelName: "QL-810W",
		MediaType: "Continuous length tape", MediaWidth: 29,
		StatusType: "Reply to status request", PhaseType: "Waiting to receive",
		Errors: []string{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("status =\n%+v\nwant\n%+v", got, want)
	}
}

func TestQueryIPPStatusRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := queryIPPStatus(server.URL + "/ipp/print"); err == nil {
		t.Fatal("queryIPPStatus accepted an HTTP 404")
	}
}
