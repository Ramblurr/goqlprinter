package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// FontFamily describes one font family available to SVG rendering. Family is
// the name an SVG font-family attribute must use.
type FontFamily struct {
	Family      string     `json:"family"`
	Source      string     `json:"source"`
	Category    []string   `json:"category"`
	Designer    string     `json:"designer"`
	License     string     `json:"license"`
	Subsets     []string   `json:"subsets"`
	Styles      []string   `json:"styles"`
	Weights     []int      `json:"weights"`
	Axes        []FontAxis `json:"axes"`
	Description string     `json:"description"`
}

// FontAxis is a variable font axis range.
type FontAxis struct {
	Tag string  `json:"tag"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// LoadFontCatalog reads and validates the font catalog JSON at path. An empty
// path yields no families.
func LoadFontCatalog(path string) ([]FontFamily, error) {
	if path == "" {
		return []FontFamily{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("font catalog: %w", err)
	}
	invalid := func(format string, a ...any) error {
		return fmt.Errorf("font catalog %s: "+format, append([]any{path}, a...)...)
	}

	var catalog struct {
		Families *[]FontFamily `json:"families"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&catalog); err != nil {
		return nil, invalid("%w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, invalid("unexpected data after the catalog object")
	}
	if catalog.Families == nil {
		return nil, invalid(`missing "families" array`)
	}
	seen := map[string]bool{}
	for i, f := range *catalog.Families {
		if f.Family == "" {
			return nil, invalid("families[%d] has no family name", i)
		}
		if seen[f.Family] {
			return nil, invalid("family %q is listed twice", f.Family)
		}
		seen[f.Family] = true
	}
	return *catalog.Families, nil
}
