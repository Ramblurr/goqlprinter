package services_test

import (
	"os"
	"path/filepath"
	"testing"

	"goqlprinter/internal/services"
)

const validCatalog = `{"families": [
  {"family": "DejaVu Sans", "source": "bundled", "category": [], "designer": "", "license": "",
   "subsets": [], "styles": ["normal", "italic"], "weights": [200, 400, 700], "axes": [], "description": ""},
  {"family": "Oswald", "source": "google-fonts", "category": ["SANS_SERIF"], "designer": "Vernon Adams",
   "license": "OFL", "subsets": ["latin"], "styles": ["normal"], "weights": [400],
   "axes": [{"tag": "wght", "min": 200, "max": 700}], "description": "Oswald is a reworking."}
]}`

func writeCatalog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFontCatalog_NoPathMeansNoFamilies(t *testing.T) {
	t.Parallel()

	families, err := services.LoadFontCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	if families == nil || len(families) != 0 {
		t.Fatalf("families = %#v, want an empty non-nil slice", families)
	}
}

func TestLoadFontCatalog_Valid(t *testing.T) {
	t.Parallel()

	families, err := services.LoadFontCatalog(writeCatalog(t, validCatalog))
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 2 {
		t.Fatalf("got %d families, want 2", len(families))
	}
	oswald := families[1]
	if oswald.Family != "Oswald" || len(oswald.Axes) != 1 || oswald.Axes[0] != (services.FontAxis{Tag: "wght", Min: 200, Max: 700}) {
		t.Fatalf("Oswald entry = %#v", oswald)
	}
}

func TestLoadFontCatalog_RejectsMalformed(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"not json":         `{"families": [`,
		"missing families": `{}`,
		"unknown field":    `{"families": [{"family": "Oswald", "weight": 400}]}`,
		"wrong type":       `{"families": [{"family": "Oswald", "weights": ["400"]}]}`,
		"empty family":     `{"families": [{"family": ""}]}`,
		"duplicate family": `{"families": [{"family": "Oswald"}, {"family": "Oswald"}]}`,
		"trailing data":    `{"families": []} {}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := services.LoadFontCatalog(writeCatalog(t, body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadFontCatalog_MissingFile(t *testing.T) {
	t.Parallel()

	if _, err := services.LoadFontCatalog(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("expected an error")
	}
}
