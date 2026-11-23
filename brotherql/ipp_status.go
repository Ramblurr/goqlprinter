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

const (
	escMediaNone       byte = 0x00
	escMediaContinuous byte = 0x0A
	escMediaDieCut     byte = 0x0B
	escStatusReply     byte = 0x00
	escPhaseWaiting    byte = 0x00
	escPhasePrinting   byte = 0x01
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

func statusFromIPP(attrs ipp.Attributes) PrinterStatus {
	status := PrinterStatus{
		ModelName:  strings.TrimPrefix(ippString(attrs, ipp.AttributePrinterMakeAndModel), "Brother "),
		MediaType:  statusMediaTypes[escMediaNone],
		StatusType: statusTypes[escStatusReply],
		Errors:     []string{},
	}

	if media := ippString(attrs, "media-ready"); media != "" {
		width, length := parseMediaSize(media)
		status.MediaWidth = width
		status.MediaLength = length
		if length == 0 {
			status.MediaType = statusMediaTypes[escMediaContinuous]
		} else {
			status.MediaType = statusMediaTypes[escMediaDieCut]
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
		status.PhaseType = statusPhaseTypes[escPhaseWaiting]
	case ippStateProcessing:
		status.Busy = true
		status.PhaseType = statusPhaseTypes[escPhasePrinting]
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
		s, _ := attr.Value.(string)
		if attr.Tag == ipp.TagTextLang || attr.Tag == ipp.TagNameLang {
			s = withoutLanguage(s)
		}
		if s != "" {
			return s
		}
	}
	return ""
}

// withoutLanguage returns the text of a textWithLanguage or nameWithLanguage
// value, which go-ipp leaves as two length-prefixed strings: language, text.
func withoutLanguage(v string) string {
	if len(v) < 2 {
		return ""
	}
	start := 2 + (int(v[0])<<8 | int(v[1]))
	if len(v) < start+2 {
		return ""
	}
	text := v[start:]
	n := int(text[0])<<8 | int(text[1])
	if len(text) < 2+n {
		return ""
	}
	return text[2 : 2+n]
}

func parseMediaSize(media string) (width, length int) {
	m := mediaSizePattern.FindStringSubmatch(media)
	if m == nil {
		return 0, 0
	}
	w, _ := strconv.ParseFloat(m[1], 64)
	l, _ := strconv.ParseFloat(m[2], 64)
	return int(math.Round(w)), int(math.Round(l))
}
