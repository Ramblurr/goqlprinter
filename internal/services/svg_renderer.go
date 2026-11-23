package services

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const svgRenderTimeout = 30 * time.Second

type SVGRenderer struct {
	resvg    string
	fontDirs []string
}

func NewSVGRenderer(fontDirs []string) (*SVGRenderer, error) {
	path, err := exec.LookPath("resvg")
	if err != nil {
		return nil, fmt.Errorf("SVG rendering needs the resvg binary on PATH: %w", err)
	}
	return &SVGRenderer{resvg: path, fontDirs: fontDirs}, nil
}

func (r *SVGRenderer) Render(ctx context.Context, svg string, maxWidth, maxHeight int, scale float64) (image.Image, error) {
	natW, natH, err := inspectSVG(svg)
	if err != nil {
		return nil, err
	}

	w := natW * scale
	h := natH * scale
	if maxWidth > 0 && w > float64(maxWidth) {
		h *= float64(maxWidth) / w
		w = float64(maxWidth)
	}
	if maxHeight > 0 && h > float64(maxHeight) {
		w *= float64(maxHeight) / h
		h = float64(maxHeight)
	}

	iw, ih := int(w), int(h)
	if iw <= 0 || ih <= 0 {
		return nil, fmt.Errorf("SVG has zero dimensions after scaling (size: %gx%g, scale: %.2f)", natW, natH, scale)
	}

	args := []string{
		"--skip-system-fonts",
		"--background", "white",
		"-w", strconv.Itoa(iw),
		"-h", strconv.Itoa(ih),
		"--font-family", "DejaVu Sans",
		"--sans-serif-family", "DejaVu Sans",
		"--serif-family", "DejaVu Serif",
		"--monospace-family", "DejaVu Sans Mono",
	}
	for _, dir := range r.fontDirs {
		args = append(args, "--use-fonts-dir", dir)
	}
	args = append(args, "-", "-c")

	ctx, cancel := context.WithTimeout(ctx, svgRenderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.resvg, args...)
	cmd.Stdin = strings.NewReader(svg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("resvg did not finish: %w", ctx.Err())
		}
		return nil, fmt.Errorf("resvg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	img, err := png.Decode(&stdout)
	if err != nil {
		return nil, fmt.Errorf("resvg output is not a PNG: %w", err)
	}
	return img, nil
}

// inspectSVG returns the SVG's natural size and rejects references outside the
// document, because resvg would read any image path on the server.
func inspectSVG(svg string) (float64, float64, error) {
	dec := xml.NewDecoder(strings.NewReader(svg))
	var w, h float64
	sawRoot := false
	for {
		tok, err := dec.Token()
		if err == io.EOF && sawRoot {
			return w, h, nil
		}
		if err != nil {
			return 0, 0, fmt.Errorf("SVG parse failed: %w", err)
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, a := range el.Attr {
			if a.Name.Local == "href" && !isInternalReference(a.Value) {
				return 0, 0, fmt.Errorf("SVG references %q; only #fragment and data: references are allowed", a.Value)
			}
		}
		if sawRoot {
			continue
		}
		sawRoot = true
		if w, h, err = rootSize(el); err != nil {
			return 0, 0, err
		}
	}
}

func isInternalReference(v string) bool {
	v = strings.TrimSpace(v)
	return strings.HasPrefix(v, "#") || strings.HasPrefix(strings.ToLower(v), "data:")
}

// rootSize returns the size resvg fits into -w/-h: the root viewport when
// width and height are both pixel lengths, else the viewBox.
func rootSize(root xml.StartElement) (float64, float64, error) {
	if root.Name.Local != "svg" {
		return 0, 0, fmt.Errorf("SVG parse failed: root element is <%s>, want <svg>", root.Name.Local)
	}
	attrs := map[string]string{}
	for _, a := range root.Attr {
		if a.Name.Space == "" {
			attrs[a.Name.Local] = a.Value
		}
	}
	w, errW := parsePixels(attrs["width"])
	h, errH := parsePixels(attrs["height"])
	if errW == nil && errH == nil {
		return w, h, nil
	}
	if w, h, ok := parseViewBoxSize(attrs["viewBox"]); ok {
		return w, h, nil
	}
	return 0, 0, fmt.Errorf("SVG has no viewBox and no usable width/height: %w", errors.Join(errW, errH))
}

func parseViewBoxSize(v string) (float64, float64, bool) {
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if len(fields) != 4 {
		return 0, 0, false
	}
	w, errW := strconv.ParseFloat(fields[2], 64)
	h, errH := strconv.ParseFloat(fields[3], 64)
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

func parsePixels(v string) (float64, error) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "px"), 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%q is not a positive pixel length", v)
	}
	return n, nil
}
