package brotherql

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	ipp "github.com/phin1x/go-ipp"
)

const (
	ippStateIdle       = 3
	ippStateProcessing = 4
	ippStateStopped    = 5
)

var ippStatusClient = &http.Client{Timeout: 2 * time.Second}

var ippStatusAttributes = []string{
	ipp.AttributePrinterState,
	ipp.AttributePrinterStateReasons,
	"media-ready",
	ipp.AttributePrinterMakeAndModel,
}

var ippReasonErrors = map[string]string{
	"media-needed": "No media when printing",
	"media-empty":  "End of media",
	"media-jam":    "Media cannot be fed",
	"cover-open":   "Cover opened while printing",
	"door-open":    "Cover opened while printing",
}

var mediaSizePattern = regexp.MustCompile(`_([0-9.]+)x([0-9.]+)mm$`)

// queryIPPStatus asks the printer at url (http://host:631/path) for its state
// with an IPP Get-Printer-Attributes request.
func queryIPPStatus(url string) (PrinterStatus, error) {
	req := ipp.NewRequest(ipp.OperationGetPrinterAttributes, 1)
	req.OperationAttributes[ipp.AttributePrinterURI] = "ipp://" + strings.TrimPrefix(url, "http://")
	req.OperationAttributes[ipp.AttributeRequestedAttributes] = ippStatusAttributes
	payload, err := req.Encode()
	if err != nil {
		return PrinterStatus{}, err
	}

	resp, err := ippStatusClient.Post(url, ipp.ContentTypeIPP, bytes.NewReader(payload))
	if err != nil {
		return PrinterStatus{}, fmt.Errorf("IPP status request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PrinterStatus{}, fmt.Errorf("IPP status request failed: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return PrinterStatus{}, fmt.Errorf("IPP status response: %w", err)
	}

	ippResp, err := ipp.NewResponseDecoder(bytes.NewReader(body)).Decode(nil)
	if err != nil {
		return PrinterStatus{}, fmt.Errorf("IPP status response: %w", err)
	}
	if err := ippResp.CheckForErrors(); err != nil {
		return PrinterStatus{}, fmt.Errorf("IPP status response: %w", err)
	}
	if len(ippResp.PrinterAttributes) == 0 {
		return PrinterStatus{}, fmt.Errorf("IPP status response has no printer attributes")
	}
	return statusFromIPP(ippResp.PrinterAttributes[0]), nil
}

// statusFromIPP maps IPP printer attributes onto the fields ParseStatusResponse
// fills from an ESC i S reply, so the frontend reads both the same way.
func statusFromIPP(attrs ipp.Attributes) PrinterStatus {
	status := PrinterStatus{
		ModelName:  strings.TrimPrefix(ippString(attrs, ipp.AttributePrinterMakeAndModel), "Brother "),
		MediaType:  statusMediaTypes[0x00],
		StatusType: statusTypes[0x00],
		Errors:     []string{},
	}

	if media := ippString(attrs, "media-ready"); media != "" {
		width, length := parseMediaSize(media)
		status.MediaWidth = width
		status.MediaLength = length
		if length == 0 {
			status.MediaType = statusMediaTypes[0x0A]
		} else {
			status.MediaType = statusMediaTypes[0x0B]
		}
	}

	for _, attr := range attrs[ipp.AttributePrinterStateReasons] {
		reason, _ := attr.Value.(string)
		if reason == "" || reason == "none" || strings.HasSuffix(reason, "-report") || strings.HasSuffix(reason, "-warning") {
			continue
		}
		if msg, ok := ippReasonErrors[strings.TrimSuffix(reason, "-error")]; ok {
			status.Errors = append(status.Errors, msg)
		} else {
			status.Errors = append(status.Errors, reason)
		}
	}

	state := 0
	if values := attrs[ipp.AttributePrinterState]; len(values) > 0 {
		state, _ = values[0].Value.(int)
	}
	switch state {
	case ippStateIdle:
		status.Ready = true
		status.PhaseType = statusPhaseTypes[0x00]
	case ippStateProcessing:
		status.Busy = true
		status.PhaseType = statusPhaseTypes[0x01]
	case ippStateStopped:
		if len(status.Errors) == 0 {
			status.Errors = append(status.Errors, "stopped")
		}
	}

	if len(status.Errors) > 0 {
		status.Error = status.Errors[0]
		status.Ready = false
		status.Busy = false
	}
	return status
}

func ippString(attrs ipp.Attributes, name string) string {
	for _, attr := range attrs[name] {
		if s, ok := attr.Value.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// parseMediaSize reads the dimensions from a PWG media name such as
// roll_current_62x29mm. A length of 0 means continuous tape.
func parseMediaSize(media string) (width, length int) {
	m := mediaSizePattern.FindStringSubmatch(media)
	if m == nil {
		return 0, 0
	}
	w, _ := strconv.ParseFloat(m[1], 64)
	l, _ := strconv.ParseFloat(m[2], 64)
	return int(math.Round(w)), int(math.Round(l))
}
